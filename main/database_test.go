package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"math/rand"
	"sync"
	"testing"

	"github.com/google/uuid"
)

const (
	TestTableName = "test_cose_identity"
)

func TestDatabaseManager(t *testing.T) {
	dm, err := initDB()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanUp(t, dm)

	testIdentity := generateRandomIdentity()

	// check not exists
	exists, err := dm.ExistsPublicKey(testIdentity.Uid)
	if err != nil {
		t.Error(err)
	}
	if exists {
		t.Error("ExistsPublicKey returned TRUE")
	}

	exists, err = dm.ExistsPrivateKey(testIdentity.Uid)
	if err != nil {
		t.Error(err)
	}
	if exists {
		t.Error("ExistsPrivateKey returned TRUE")
	}

	exists, err = dm.ExistsUuidForPublicKey(testIdentity.PublicKey)
	if err != nil {
		t.Error(err)
	}
	if exists {
		t.Error("ExistsUuidForPublicKey returned TRUE")
	}

	// store identity
	tx, err := dm.StartTransaction(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	err = dm.StoreNewIdentity(tx, *testIdentity)
	if err != nil {
		t.Fatal(err)
	}

	err = dm.CloseTransaction(tx, Commit)
	if err != nil {
		t.Fatal(err)
	}

	// check exists
	exists, err = dm.ExistsPublicKey(testIdentity.Uid)
	if err != nil {
		t.Error(err)
	}
	if !exists {
		t.Error("ExistsPublicKey returned FALSE")
	}

	exists, err = dm.ExistsPrivateKey(testIdentity.Uid)
	if err != nil {
		t.Error(err)
	}
	if !exists {
		t.Error("ExistsPrivateKey returned FALSE")
	}

	exists, err = dm.ExistsUuidForPublicKey(testIdentity.PublicKey)
	if err != nil {
		t.Error(err)
	}
	if !exists {
		t.Error("ExistsUuidForPublicKey returned FALSE")
	}

	// get attributes
	auth, err := dm.GetAuthToken(testIdentity.Uid)
	if err != nil {
		t.Error(err)
	}
	if auth != testIdentity.AuthToken {
		t.Error("GetAuthToken returned unexpected value")
	}

	priv, err := dm.GetPrivateKey(testIdentity.Uid)
	if err != nil {
		t.Error(err)
	}
	if !bytes.Equal(priv, testIdentity.PrivateKey) {
		t.Error("GetPrivateKey returned unexpected value")
	}

	pub, err := dm.GetPublicKey(testIdentity.Uid)
	if err != nil {
		t.Error(err)
	}
	if !bytes.Equal(pub, testIdentity.PublicKey) {
		t.Error("GetPublicKey returned unexpected value")
	}

	uid, err := dm.GetUuidForPublicKey(testIdentity.PublicKey)
	if err != nil {
		t.Error(err)
	}
	if !bytes.Equal(uid[:], testIdentity.Uid[:]) {
		t.Error("GetUuidForPublicKey returned unexpected value")
	}
}

func TestDatabaseLoad(t *testing.T) {
	wg := &sync.WaitGroup{}

	dm, err := initDB()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanUp(t, dm)

	// generate identities
	var testIdentities []*Identity
	for i := 0; i < 1000; i++ {
		testIdentities = append(testIdentities, generateRandomIdentity())
	}

	// store identities
	for _, testId := range testIdentities {
		go func(id *Identity) {
			err := storeIdentity(dm, id, wg)
			if err != nil {
				t.Errorf("%s: %v", id.Uid, err)
			}
		}(testId)
	}
	wg.Wait()

	// check identities
	for _, testId := range testIdentities {
		go func(id *Identity) {
			err := checkIdentity(dm, id, wg)
			if err != nil {
				t.Errorf("%s: %v", id.Uid, err)
			}
		}(testId)
	}
	wg.Wait()
}

func initDB() (*DatabaseManager, error) {
	conf := &Config{}
	err := conf.Load("", "config.json")
	if err != nil {
		return nil, fmt.Errorf("ERROR: unable to load configuration: %s", err)
	}

	return NewSqlDatabaseInfo(conf.PostgresDSN, TestTableName)
}

func cleanUp(t *testing.T, dm *DatabaseManager) {
	dropTableQuery := fmt.Sprintf("DROP TABLE %s;", TestTableName)
	_, err := dm.db.Exec(dropTableQuery)
	if err != nil {
		t.Error(err)
	}
}

func generateRandomIdentity() *Identity {
	priv := make([]byte, 32)
	rand.Read(priv)

	pub := make([]byte, 64)
	rand.Read(pub)

	auth := make([]byte, 16)
	rand.Read(auth)

	return &Identity{
		Uid:        uuid.New(),
		PrivateKey: priv,
		PublicKey:  pub,
		AuthToken:  base64.StdEncoding.EncodeToString(auth),
	}
}

func storeIdentity(dm *DatabaseManager, id *Identity, wg *sync.WaitGroup) error {
	wg.Add(1)
	defer wg.Done()

	tx, err := dm.StartTransaction(context.Background())
	if err != nil {
		return err
	}

	err = dm.StoreNewIdentity(tx, *id)
	if err != nil {
		return err
	}

	err = dm.CloseTransaction(tx, Commit)
	if err != nil {
		return err
	}

	return nil
}

func checkIdentity(dm *DatabaseManager, id *Identity, wg *sync.WaitGroup) error {
	wg.Add(1)
	defer wg.Done()

	exists, err := dm.ExistsPublicKey(id.Uid)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("ExistsPublicKey returned FALSE")
	}

	exists, err = dm.ExistsPrivateKey(id.Uid)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("ExistsPrivateKey returned FALSE")
	}

	exists, err = dm.ExistsUuidForPublicKey(id.PublicKey)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("ExistsUuidForPublicKey returned FALSE")
	}

	auth, err := dm.GetAuthToken(id.Uid)
	if err != nil {
		return err
	}
	if auth != id.AuthToken {
		return fmt.Errorf("GetAuthToken returned unexpected value: %s, expected: %s", auth, id.AuthToken)
	}

	priv, err := dm.GetPrivateKey(id.Uid)
	if err != nil {
		return err
	}
	if !bytes.Equal(priv, id.PrivateKey) {
		return fmt.Errorf("GetPrivateKey returned unexpected value: %s, expected: %s", priv, id.PrivateKey)
	}

	pub, err := dm.GetPublicKey(id.Uid)
	if err != nil {
		return err
	}
	if !bytes.Equal(pub, id.PublicKey) {
		return fmt.Errorf("GetPublicKey returned unexpected value: %s, expected: %s", pub, id.PublicKey)
	}

	uid, err := dm.GetUuidForPublicKey(id.PublicKey)
	if err != nil {
		return err
	}
	if !bytes.Equal(uid[:], id.Uid[:]) {
		return fmt.Errorf("GetUuidForPublicKey returned unexpected value: %s, expected: %s", uid, id.Uid)
	}

	return nil
}