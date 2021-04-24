package main

import (
	"github.com/google/uuid"
)

type ContextManager interface {
	Exists(uid uuid.UUID) bool

	StartTransaction(uid uuid.UUID) error
	EndTransaction(uid uuid.UUID, success bool) error

	GetPrivateKey(uid uuid.UUID) (privKey []byte, err error)
	SetPrivateKey(uid uuid.UUID, privKey []byte) error

	GetPublicKey(uid uuid.UUID) (pubKey []byte, err error)
	SetPublicKey(uid uuid.UUID, pubKey []byte) error
}
