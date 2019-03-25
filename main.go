package main

import (
	"fmt"
	"os"

	glog "log"

	"github.com/ovrclk/hack/kvs"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	cfg "github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/node"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/privval"
)

var (
	config = cfg.DefaultConfig()
	logger = log.NewTMLogger(log.NewSyncWriter(os.Stdout))
)

var cmd = &cobra.Command{
	Use: "hack",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) (err error) {
		config, err = ParseConfig()
		if err != nil {
			return err
		}
		return nil
	},
}

var nodecmd = &cobra.Command{
	Use: "node",
	RunE: func(cmd *cobra.Command, args []string) error {
		n, err := NewNode(config, logger)
		if err != nil {
			return err
		}
		if err := n.Start(); err != nil {
			return fmt.Errorf("failed to start node: %v", err)
		}
		logger.Info("started node", "nodeInfo", n.Switch().NodeInfo())
		return nil
	},
}

func main() {
	cmd.AddCommand(nodecmd)
	if err := cmd.Execute(); err != nil {
		glog.Fatalf("error: %v", err)
		//panic(err)
		//log.Error(err)
	}
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
