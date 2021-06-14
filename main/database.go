// Copyright (c) 2019-2020 ubirch GmbH
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/google/uuid"
	"github.com/lib/pq"
	log "github.com/sirupsen/logrus"
	"time"
	// postgres driver is imported for side effects
	// import pq driver this way only if we dont need it here
	// done for database/sql (pg, err := sql.Open..)
	//_ "github.com/lib/pq"
)

const (
	PostgreSql                  string = "postgres"
	PostgreSqlIdentityTableName string = "cose_identity"
)

const (
	PostgresIdentity = iota
)

var create = map[int]string{
	PostgresIdentity: "CREATE TABLE IF NOT EXISTS %s(" +
		"uid VARCHAR(255) NOT NULL PRIMARY KEY, " +
		"private_key BYTEA NOT NULL, " +
		"public_key BYTEA NOT NULL, " +
		"auth_token VARCHAR(255) NOT NULL);",
}

func CreateTable(tableType int, tableName string) string {
	return fmt.Sprintf(create[tableType], tableName)
}

// DatabaseManager contains the postgres database connection, and offers methods
// for interacting with the database.
type DatabaseManager struct {
	options   *sql.TxOptions
	db        *sql.DB
	tableName string
}

// Ensure Database implements the ContextManager interface
var _ ContextManager = (*DatabaseManager)(nil)

// NewSqlDatabaseInfo takes a database connection string, returns a new initialized
// database.
func NewSqlDatabaseInfo(dataSourceName, tableName string) (*DatabaseManager, error) {
	pg, err := sql.Open(PostgreSql, dataSourceName)
	if err != nil {
		return nil, err
	}
	pg.SetMaxOpenConns(100)
	pg.SetMaxIdleConns(75)
	pg.SetConnMaxLifetime(10 * time.Minute)
	if err = pg.Ping(); err != nil {
		return nil, err
	}

	log.Print("preparing postgres usage")

	dbManager := &DatabaseManager{
		options: &sql.TxOptions{
			Isolation: sql.LevelReadCommitted,
			ReadOnly:  false,
		},
		db:        pg,
		tableName: tableName,
	}

	if _, err = dbManager.db.Exec(CreateTable(PostgresIdentity, tableName)); err != nil {
		return nil, err
	}

	return dbManager, nil
}

func (dm *DatabaseManager) Exists(uid uuid.UUID) (bool, error) {
	var buf uuid.UUID

	query := fmt.Sprintf("SELECT uid FROM %s WHERE uid = $1", dm.tableName)

	err := dm.db.QueryRow(query, uid.String()).Scan(&buf)
	if err != nil {
		if dm.isConnectionAvailable(err) {
			return dm.Exists(uid)
		}
		if err == sql.ErrNoRows {
			return false, nil
		} else {
			return false, err
		}
	} else {
		return true, nil
	}
}

func (dm *DatabaseManager) ExistsUuidForPublicKey(pubKey []byte) (bool, error) {
	var uid uuid.UUID

	query := fmt.Sprintf("SELECT uid FROM %s WHERE public_key = $1", dm.tableName)

	err := dm.db.QueryRow(query, pubKey).Scan(&uid)
	if err != nil {
		if dm.isConnectionAvailable(err) {
			return dm.ExistsUuidForPublicKey(pubKey)
		}
		if err == sql.ErrNoRows {
			return false, nil
		} else {
			return false, err
		}
	} else {
		return true, nil
	}
}

func (dm *DatabaseManager) ExistsPrivateKey(uid uuid.UUID) (bool, error) {
	var privateKey []byte

	query := fmt.Sprintf("SELECT private_key FROM %s WHERE uid = $1", dm.tableName)

	err := dm.db.QueryRow(query, uid.String()).Scan(&privateKey)
	if err != nil {
		if dm.isConnectionAvailable(err) {
			return dm.ExistsPrivateKey(uid)
		}
		if err == sql.ErrNoRows || len(privateKey) == 0 {
			return false, nil
		} else {
			return false, err
		}
	} else {
		return true, nil
	}
}

func (dm *DatabaseManager) ExistsPublicKey(uid uuid.UUID) (bool, error) {
	var publicKey []byte

	query := fmt.Sprintf("SELECT public_key FROM %s WHERE uid = $1", dm.tableName)

	err := dm.db.QueryRow(query, uid.String()).Scan(&publicKey)
	if err != nil {
		if dm.isConnectionAvailable(err) {
			return dm.ExistsPublicKey(uid)
		}
		if err == sql.ErrNoRows || len(publicKey) == 0 {
			return false, nil
		} else {
			return false, err
		}
	} else {
		return true, nil
	}
}

func (dm *DatabaseManager) GetUuidForPublicKey(pubKey []byte) (uuid.UUID, error) {
	var uid uuid.UUID

	query := fmt.Sprintf("SELECT uid FROM %s WHERE public_key = $1", dm.tableName)

	err := dm.db.QueryRow(query, pubKey).Scan(&uid)
	if err != nil {
		if dm.isConnectionAvailable(err) {
			return dm.GetUuidForPublicKey(pubKey)
		}
		return uuid.Nil, err
	}

	return uid, nil
}

func (dm *DatabaseManager) GetPrivateKey(uid uuid.UUID) ([]byte, error) {
	var privateKey []byte

	query := fmt.Sprintf("SELECT private_key FROM %s WHERE uid = $1", dm.tableName)

	err := dm.db.QueryRow(query, uid.String()).Scan(&privateKey)
	if err != nil {
		if dm.isConnectionAvailable(err) {
			return dm.GetPrivateKey(uid)
		}
		return nil, err
	}

	return privateKey, nil
}

func (dm *DatabaseManager) GetPublicKey(uid uuid.UUID) ([]byte, error) {
	var publicKey []byte

	query := fmt.Sprintf("SELECT public_key FROM %s WHERE uid = $1", dm.tableName)

	err := dm.db.QueryRow(query, uid.String()).Scan(&publicKey)
	if err != nil {
		if dm.isConnectionAvailable(err) {
			return dm.GetPublicKey(uid)
		}
		return nil, err
	}

	return publicKey, nil
}

func (dm *DatabaseManager) GetAuthToken(uid uuid.UUID) (string, error) {
	var authToken string

	query := fmt.Sprintf("SELECT auth_token FROM %s WHERE uid = $1", dm.tableName)

	err := dm.db.QueryRow(query, uid.String()).Scan(&authToken)
	if err != nil {
		if dm.isConnectionAvailable(err) {
			return dm.GetAuthToken(uid)
		}
		return "", err
	}

	return authToken, nil
}

func (dm *DatabaseManager) StartTransaction(ctx context.Context) (transactionCtx interface{}, err error) {
	return dm.db.BeginTx(ctx, dm.options)
}

func (dm *DatabaseManager) CloseTransaction(transactionCtx interface{}, commit bool) error {
	tx, ok := transactionCtx.(*sql.Tx)
	if !ok {
		return fmt.Errorf("transactionCtx for database manager is not of expected type *sql.Tx")
	}

	if commit {
		return tx.Commit()
	} else {
		return tx.Rollback()
	}
}

func (dm *DatabaseManager) SetAuthToken(transactionCtx interface{}, uid uuid.UUID, authToken string) error {
	tx, ok := transactionCtx.(*sql.Tx)
	if !ok {
		return fmt.Errorf("transactionCtx for database manager is not of expected type *sql.Tx")
	}

	query := fmt.Sprintf("UPDATE %s SET auth_token = $1 WHERE uid = $2;", dm.tableName)

	_, err := tx.Exec(query, &authToken, uid.String())
	if err != nil {
		if dm.isConnectionAvailable(err) {
			return dm.SetAuthToken(tx, uid, authToken)
		}
		return err
	}

	return nil
}

func (dm *DatabaseManager) SetPublicKey(transactionCtx interface{}, uid uuid.UUID, pub string) error {
	tx, ok := transactionCtx.(*sql.Tx)
	if !ok {
		return fmt.Errorf("transactionCtx for database manager is not of expected type *sql.Tx")
	}

	query := fmt.Sprintf("UPDATE %s SET public_key = $1 WHERE uid = $2;", dm.tableName)

	_, err := tx.Exec(query, &pub, uid.String())
	if err != nil {
		if dm.isConnectionAvailable(err) {
			return dm.SetPublicKey(tx, uid, pub)
		}
		return err
	}

	return nil
}

func (dm *DatabaseManager) StoreNewIdentity(transactionCtx interface{}, identity Identity) error {
	tx, ok := transactionCtx.(*sql.Tx)
	if !ok {
		return fmt.Errorf("transactionCtx for database manager is not of expected type *sql.Tx")
	}

	query := fmt.Sprintf(
		"INSERT INTO %s (uid, private_key, public_key, auth_token) VALUES ($1, $2, $3, $4);",
		dm.tableName)

	_, err := tx.Exec(query, &identity.Uid, &identity.PrivateKey, &identity.PublicKey, &identity.AuthToken)
	if err != nil {
		if dm.isConnectionAvailable(err) {
			return dm.StoreNewIdentity(tx, identity)
		}
		return err
	}

	return nil
}

func (dm *DatabaseManager) isConnectionAvailable(err error) bool {
	if err.Error() == pq.ErrorCode("53300").Name() || // "53300": "too_many_connections",
		err.Error() == pq.ErrorCode("53400").Name() { // "53400": "configuration_limit_exceeded",
		time.Sleep(100 * time.Millisecond)
		return true
	}
	return false
}
