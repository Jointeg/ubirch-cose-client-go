package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

const (
	Commit   = true
	Rollback = false
)

var (
	ErrExists   = errors.New("entry already exists")
	ErrNotExist = errors.New("entry does not exist")
)

type ContextManager interface {
	StartTransaction(ctx context.Context) (transactionCtx interface{}, err error)
	CloseTransaction(transactionCtx interface{}, commit bool) error

	StoreNewIdentity(tx interface{}, id Identity) error

	ExistsPrivateKey(uid uuid.UUID) (bool, error)
	GetPrivateKey(uid uuid.UUID) (privKey []byte, err error)
	//SetPrivateKey(tx interface{}, uid uuid.UUID, privKey []byte) error

	ExistsPublicKey(uid uuid.UUID) (bool, error)
	GetPublicKey(uid uuid.UUID) (pubKey []byte, err error)
	//SetPublicKey(tx interface{}, uid uuid.UUID, pubKey []byte) error

	GetAuthToken(uid uuid.UUID) (string, error)
	SetAuthToken(transactionCtx interface{}, uid uuid.UUID, authToken string) error

	ExistsUuidForPublicKey(pubKey []byte) (bool, error)
	GetUuidForPublicKey(pubKey []byte) (uuid.UUID, error)
}

func GetCtxManager(c *Config) (ContextManager, error) {
	if c.PostgresDSN != "" {
		return NewSqlDatabaseInfo(c.PostgresDSN, PostgreSqlIdentityTableName)
	} else {
		return nil, fmt.Errorf("file-based context management is not supported in the current version")
	}
}
