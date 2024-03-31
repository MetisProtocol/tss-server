package tss

import (
	"bytes"

	dbm "github.com/tendermint/tm-db"
	pb "google.golang.org/protobuf/proto"
)

type LocalStorage interface {

	// GetKeyDataListByKeyId get KeyData list by keyId, return empty list if not found
	GetKeyDataListByKeyId(keyId string) ([]*KeyData, error)

	// GetKeyDataByKeyIdAndKeySessionId get KeyData by keyId and keySessionId, return nil if not found
	GetKeyDataByKeyIdAndKeySessionId(keyId string, keySessionId string) (*KeyData, error)

	// SetKeyData set KeyData, update local storage
	SetKeyData(keyData *KeyData) error

	// DeleteKeyData delete keyData, update local storage
	DeleteKeyData(keyId string, keySessionId string) error
}

type LevelDBStorage struct {
	path string
	db   dbm.DB
}

func NewLevelDBStorage(path string) *LevelDBStorage {
	return &LevelDBStorage{path: path}
}

func (s *LevelDBStorage) Start() error {
	db := dbm.NewDB("tss", dbm.GoLevelDBBackend, s.path)
	s.db = db
	return nil
}

func (s *LevelDBStorage) Stop() {
	s.db.Close()
}

func (s *LevelDBStorage) GetKeyDataListByKeyId(keyId string) ([]*KeyData, error) {
	prefix := []byte(keyId + ":")
	itr := s.db.Iterator(prefix, nil)
	defer itr.Close()

	keyDataList := make([]*KeyData, 0)
	for ; itr.Valid(); itr.Next() {
		if !bytes.HasPrefix(itr.Key(), prefix) {
			break
		}
		keyData := &KeyData{}
		err := pb.Unmarshal(itr.Value(), keyData)
		if err != nil {
			return nil, err
		}
		keyDataList = append(keyDataList, keyData)
	}
	return keyDataList, nil
}

func (s *LevelDBStorage) GetKeyDataByKeyIdAndKeySessionId(keyId string, keySessionId string) (*KeyData, error) {
	bs := s.db.Get([]byte(keyId + ":" + keySessionId))
	if bs == nil {
		return nil, nil
	}

	keyData := &KeyData{}
	err := pb.Unmarshal(bs, keyData)
	if err != nil {
		return nil, err
	}
	return keyData, nil
}

func (s *LevelDBStorage) SetKeyData(keyData *KeyData) error {
	keyId := keyData.GetKeyId()
	keySessionId := keyData.GetKeySessionId()
	data, err := pb.Marshal(keyData)
	if err != nil {
		return err
	}
	s.db.SetSync([]byte(keyId+":"+keySessionId), data)
	return nil
}

func (s *LevelDBStorage) DeleteKeyData(keyId string, keySessionId string) error {
	s.db.DeleteSync([]byte(keyId + ":" + keySessionId))
	return nil
}
