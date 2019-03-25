package main

import (
	"fmt"
	"os"

	glog "log"

	"github.com/ovrclk/hack/kvs"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	cfg "github.com/tendermint/tendermint/config"
	cmn "github.com/tendermint/tendermint/libs/common"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/node"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/types"
	tmtime "github.com/tendermint/tendermint/types/time"
)

var (
	config = cfg.DefaultConfig()
	logger = log.NewTMLogger(log.NewSyncWriter(os.Stdout))

	cmd = &cobra.Command{
		Use: "hack",
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
)

func main() {
	cmd.AddCommand(nodecmd)
	cmd.AddCommand(initFilesCmd)
	if err := cmd.Execute(); err != nil {
		glog.Fatalf("error: %v", err)
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
	conf.SetRoot(cfg.DefaultTendermintDir)
	err := viper.Unmarshal(conf)
	if err != nil {
		return nil, err
	}
	conf.SetRoot(conf.RootDir)
	cfg.EnsureRoot(conf.RootDir)
	if err = conf.ValidateBasic(); err != nil {
		return nil, fmt.Errorf("Error in config file: %v", err)
	}
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

	// Get Blockstore
	// blockStoreDB, err := node.DefaultDBProvider(&node.DBContext{ID: "blockstore", Config: config})
	// if err != nil {
	// 	return nil, err
	// }

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
