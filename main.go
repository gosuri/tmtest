package main

import (
	"context"
	"fmt"
	"html"
	"os"

	glog "log"

	"github.com/davecgh/go-spew/spew"
	"github.com/gosuri/uitable"
	"github.com/ovrclk/hack/kvs"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	cfg "github.com/tendermint/tendermint/config"
	cmn "github.com/tendermint/tendermint/libs/common"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/node"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/privval"
	tmclient "github.com/tendermint/tendermint/rpc/client"
	libclient "github.com/tendermint/tendermint/rpc/lib/client"
	"github.com/tendermint/tendermint/types"
	tmtime "github.com/tendermint/tendermint/types/time"
)

const maxTokens uint64 = 1000000000000000

var (
	baseDir = ".tendermint"
	config  = cfg.DefaultConfig()
	logger  = log.NewTMLogger(log.NewSyncWriter(os.Stdout))

	cmd = &cobra.Command{
		Use: "tmtest",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) (err error) {
			config, err = ParseConfig()
			if err != nil {
				return err
			}
			return nil
		},
	}

	nodecmd = &cobra.Command{
		Use: "node",
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := NewNode(config, logger)
			if err != nil {
				return err
			}

			cmn.TrapSignal(logger, func() {
				if n.IsRunning() {
					n.Stop()
				}
			})

			if err := n.Start(); err != nil {
				return fmt.Errorf("failed to start node: %v", err)
			}
			logger.Info("started node", "nodeInfo", n.Switch().NodeInfo())

			select {}
		},
	}

	initFilesCmd = &cobra.Command{
		Use:   "init",
		Short: "Initialize Tendermint",
		RunE: func(cmd *cobra.Command, args []string) error {
			return initFilesWithConfig(config)
		},
	}

	getCmd = &cobra.Command{
		Use:   "get",
		Short: "Get a key value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return doGet(args[0])
		},
	}
	setCmd = &cobra.Command{
		Use:   "set",
		Short: "set a key value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return doSet(args[0], args[1])
		},
	}
	watchCmd = &cobra.Command{
		Use:   "watch",
		Short: "watch",
		RunE: func(cmd *cobra.Command, args []string) error {
			return doWatch()
		},
	}
)

func init() {
	cmd.PersistentFlags().StringVarP(&baseDir, "basedir", "b", baseDir, "base dir")
	cmd.AddCommand(nodecmd)
	cmd.AddCommand(initFilesCmd)
	cmd.AddCommand(getCmd)
	cmd.AddCommand(setCmd)
	cmd.AddCommand(watchCmd)
}

func main() {
	if err := cmd.Execute(); err != nil {
		glog.Fatalf("error: %+v", err)
	}
}

func initFilesWithConfig(config *cfg.Config) error {
	// find or create private validate key and state
	pvkeyfile := config.PrivValidatorKeyFile()
	pvstatefile := config.PrivValidatorStateFile()
	var pv *privval.FilePV
	if cmn.FileExists(pvkeyfile) {
		pv = privval.LoadFilePV(pvkeyfile, pvstatefile)
		logger.Info("Found private validator", "keyFile", pvkeyfile,
			"stateFile", pvstatefile)
	} else {
		pv = privval.GenFilePV(pvkeyfile, pvstatefile)
		pv.Save()
		logger.Info("Generated private validator", "keyFile", pvkeyfile,
			"stateFile", pvstatefile)
	}

	// find or create node key
	nodeKeyFile := config.NodeKeyFile()
	if cmn.FileExists(nodeKeyFile) {
		logger.Info("Found node key", "path", nodeKeyFile)
	} else {
		if _, err := p2p.LoadOrGenNodeKey(nodeKeyFile); err != nil {
			return err
		}
		logger.Info("Generated node key", "path", nodeKeyFile)
	}
	// find or create genesis file
	genFile := config.GenesisFile()
	if cmn.FileExists(genFile) {
		logger.Info("Found genesis file", "path", genFile)
	} else {
		genDoc := types.GenesisDoc{
			ChainID:         fmt.Sprintf("test-chain-%v", cmn.RandStr(6)),
			GenesisTime:     tmtime.Now(),
			ConsensusParams: types.DefaultConsensusParams(),
		}
		key := pv.GetPubKey()
		genDoc.Validators = []types.GenesisValidator{{
			Address: key.Address(),
			PubKey:  key,
			Power:   10,
		}}
		if err := genDoc.SaveAs(genFile); err != nil {
			return err
		}
		logger.Info("Generated genesis file", "path", genFile)
	}
	return nil
}

// ParseConfig retrieves the default environment configuration,
// sets up the Tendermint root and ensures that the root exists
func ParseConfig() (*cfg.Config, error) {
	conf := cfg.DefaultConfig()
	conf.SetRoot(baseDir)
	err := viper.Unmarshal(conf)
	if err != nil {
		return nil, err
	}
	conf.SetRoot(conf.RootDir)
	cfg.EnsureRoot(conf.RootDir)
	if err = conf.ValidateBasic(); err != nil {
		return nil, fmt.Errorf("Error in config file: %v", err)
	}

	fmt.Printf("<!--\n" + html.EscapeString(spew.Sdump(conf)) + "\n-->")

	return conf, err
}

// NewNode creates a new tendermint node
func NewNode(config *cfg.Config, logger log.Logger) (*node.Node, error) {
	// Generate node PrivKey
	nodeKey, err := p2p.LoadOrGenNodeKey(config.NodeKeyFile())
	if err != nil {
		return nil, err
	}

	newPrivValKey := config.PrivValidatorKeyFile()
	newPrivValState := config.PrivValidatorStateFile()

	// client creator
	clientCreator, err := kvs.NewClientCreator()
	if err != nil {
		return nil, err
	}

	return node.NewNode(config,
		privval.LoadOrGenFilePV(newPrivValKey, newPrivValState),
		nodeKey,
		clientCreator,
		node.DefaultGenesisDocProviderFunc(config),
		node.DefaultDBProvider,
		node.DefaultMetricsProvider(config.Instrumentation),
		logger,
	)
}

func doGet(key string) error {
	res, err := httpClient().ABCIQuery("/", []byte(key))
	if err != nil {
		return err
	}
	table := uitable.New()
	table.MaxColWidth = 50
	table.AddRow("Key:", string(res.Response.GetKey()))
	table.AddRow("Value:", string(res.Response.GetValue()))
	table.AddRow("Proof:", res.Response.GetProof().String())
	table.AddRow("Height:", string(res.Response.GetHeight()))
	fmt.Println(table)
	return nil
}

func doSet(key, val string) error {
	dat := []byte(fmt.Sprintf("%s=%s", key, val))
	res, err := httpClient().BroadcastTxCommit(dat)
	if err != nil {
		return err
	}
	table := uitable.New()
	table.MaxColWidth = 50
	table.Wrap = true
	table.AddRow("CheckTx:", string(res.CheckTx.String()))
	table.AddRow("DeliverTx:", string(res.DeliverTx.String()))
	fmt.Println(table)
	return nil
}

func doWatch() error {
	wsc := libclient.NewWSClient("localhost:26657", "/websocket")
	if err := wsc.Start(); err != nil {
		return err
	}
	defer wsc.Stop()
	fmt.Println("---> STARTED ****")
	if err := wsc.Subscribe(context.Background(), "tm.event='NewBlock'"); err != nil {
		return err
	}
	fmt.Println("---> SUBSCRIBED ****")

	table := uitable.New()
	table.MaxColWidth = 50
	table.Wrap = true
	for {
		select {
		case y := <-wsc.ResponsesCh:
			table.AddRow("Event:", string(y.Result))
			fmt.Println(table)
		case <-wsc.Quit():
			fmt.Println("QUIT")
			return nil
		}
	}
	return nil
}

func httpClient() *tmclient.HTTP {
	return tmclient.NewHTTP("http://localhost:26657", "/websocket")
}
