package kvs

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sync"

	abcicli "github.com/tendermint/tendermint/abci/client"
	"github.com/tendermint/tendermint/abci/example/code"
	"github.com/tendermint/tendermint/abci/types"
	cmn "github.com/tendermint/tendermint/libs/common"
	dbm "github.com/tendermint/tendermint/libs/db"
	"github.com/tendermint/tendermint/proxy"
	"github.com/tendermint/tendermint/version"
)

var (
	stateKey        = []byte("stateKey")
	kvPairPrefixKey = []byte("kvPairKey:")
	// ProtocolVersion is the version of the protocol
	ProtocolVersion version.Protocol = 0x1
)

// State is the application state
type State struct {
	db      dbm.DB
	Size    int64  `json:"size"`
	Height  int64  `json:"height"`
	AppHash []byte `json:"app_hash"`
}

func prefixKey(key []byte) []byte {
	return append(kvPairPrefixKey, key...)
}

func saveState(state State) {
	stateBytes, err := json.Marshal(state)
	if err != nil {
		panic(err)
	}
	state.db.Set(stateKey, stateBytes)
}

func loadState(db dbm.DB) (State, error) {
	stateBytes := db.Get(stateKey)
	var state State
	if len(stateBytes) != 0 {
		err := json.Unmarshal(stateBytes, &state)
		if err != nil {
			return state, err
		}
	}
	state.db = db
	return state, nil
}

type clientCreator struct {
	mtx *sync.Mutex
	app types.Application
}

// NewClientCreator create a new client creator for KVStore
func NewClientCreator() (proxy.ClientCreator, error) {
	app, err := NewKVStoreApplication()
	if err != nil {
		return nil, err
	}
	return &clientCreator{mtx: new(sync.Mutex), app: app}, nil
}

func (c *clientCreator) NewABCIClient() (abcicli.Client, error) {
	return abcicli.NewLocalClient(c.mtx, c.app), nil
}

// KVStoreApplication is a tendermint key value store app
type KVStoreApplication struct {
	types.BaseApplication
	state State
}

// Info returns the application information
func (k *KVStoreApplication) Info(types.RequestInfo) types.ResponseInfo {
	return types.ResponseInfo{
		Data:       fmt.Sprintf("{\"size\":%v}", k.state.Size),
		Version:    version.ABCIVersion,
		AppVersion: ProtocolVersion.Uint64(),
	}
}

// Query returns the state of the application
func (k *KVStoreApplication) Query(reqQuery types.RequestQuery) (resQuery types.ResponseQuery) {
	// if the proof of existence if requested then return proof
	// or else return the actual value
	if reqQuery.Prove {
		resQuery.Index = -1
	}
	resQuery.Key = reqQuery.Data
	value := k.state.db.Get(prefixKey(reqQuery.Data))
	resQuery.Value = value

	if value != nil {
		resQuery.Log = "exists"
	} else {
		resQuery.Log = "missing"
	}
	return
}

// CheckTx validates a transaction for the mempool
func (k *KVStoreApplication) CheckTx(tx []byte) types.ResponseCheckTx {
	return types.ResponseCheckTx{Code: code.CodeTypeOK, GasWanted: 1}
}

// DeliverTx delivers a transaction for full processing
func (k *KVStoreApplication) DeliverTx(tx []byte) types.ResponseDeliverTx {
	var key, value []byte
	parts := bytes.Split(tx, []byte("="))
	if len(parts) == 2 {
		key, value = parts[0], parts[1]
	} else {
		key, value = tx, tx
	}
	k.state.db.Set(prefixKey(key), value)
	k.state.Size++

	tags := []cmn.KVPair{
		{Key: []byte("app.creator"), Value: []byte("Greg Osuri")},
		{Key: []byte("app.key"), Value: key},
	}
	return types.ResponseDeliverTx{Code: code.CodeTypeOK, Tags: tags}
}

// Commit commits the state and returns the applicaiton merkle root hash
func (k *KVStoreApplication) Commit() types.ResponseCommit {
	// Using a memdb - just return the big endian size of the db
	appHash := make([]byte, 8)
	binary.PutVarint(appHash, k.state.Size)
	k.state.AppHash = appHash
	k.state.Height++
	saveState(k.state)
	return types.ResponseCommit{Data: appHash}
}

// NewKVStoreApplication returns a new instance of KVStoreApplication
func NewKVStoreApplication() (*KVStoreApplication, error) {
	// load state from memory
	s, err := loadState(dbm.NewMemDB())
	if err != nil {
		return nil, err
	}
	return &KVStoreApplication{state: s}, nil
}
