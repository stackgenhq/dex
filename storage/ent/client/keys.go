package client

import (
	"context"
	"errors"

	"github.com/dexidp/dex/storage"
	"github.com/dexidp/dex/storage/ent/db"
	"github.com/dexidp/dex/storage/sqlretry"
)

func getKeys(ctx context.Context, client *db.KeysClient) (storage.Keys, error) {
	rawKeys, err := client.Get(ctx, keysRowID)
	if err != nil {
		return storage.Keys{}, convertDBError("get keys: %w", err)
	}

	return toStorageKeys(rawKeys), nil
}

// GetKeys returns signing keys, public keys and verification keys from the database.
func (d *Database) GetKeys(ctx context.Context) (storage.Keys, error) {
	return getKeys(ctx, d.client.Keys)
}

// UpdateKeys rotates keys using updater function.
//
// The update runs in a SERIALIZABLE transaction; when multiple dex instances
// rotate their signing keys concurrently, the database aborts all but one
// transaction with a serialization failure (SQLSTATE 40001/40P01). Retry the
// whole transaction on those transient errors: every attempt re-reads the
// current keys, and the updater's NextRotation check makes the losing instance
// converge to errAlreadyRotated instead of failing.
func (d *Database) UpdateKeys(ctx context.Context, updater func(old storage.Keys) (storage.Keys, error)) error {
	return sqlretry.Do(
		func() error { return d.updateKeysOnce(ctx, updater) },
		sqlretry.IsSerializationFailure,
		nil,
	)
}

func (d *Database) updateKeysOnce(ctx context.Context, updater func(old storage.Keys) (storage.Keys, error)) error {
	firstUpdate := false

	tx, err := d.BeginTx(ctx)
	if err != nil {
		return convertDBError("update keys tx: %w", err)
	}

	storageKeys, err := getKeys(ctx, tx.Keys)
	if err != nil {
		if !errors.Is(err, storage.ErrNotFound) {
			return rollback(tx, "update keys get: %w", err)
		}
		firstUpdate = true
	}

	newKeys, err := updater(storageKeys)
	if err != nil {
		return rollback(tx, "update keys updating: %w", err)
	}

	// ent doesn't have an upsert support yet
	// https://github.com/facebook/ent/issues/139
	if firstUpdate {
		_, err = tx.Keys.Create().
			SetID(keysRowID).
			SetNextRotation(newKeys.NextRotation).
			SetSigningKey(*newKeys.SigningKey).
			SetSigningKeyPub(*newKeys.SigningKeyPub).
			SetVerificationKeys(newKeys.VerificationKeys).
			Save(ctx)
		if err != nil {
			return rollback(tx, "create keys: %w", err)
		}
		if err = tx.Commit(); err != nil {
			return rollback(tx, "update keys commit: %w", err)
		}
		return nil
	}

	err = tx.Keys.UpdateOneID(keysRowID).
		SetNextRotation(newKeys.NextRotation.UTC()).
		SetSigningKey(*newKeys.SigningKey).
		SetSigningKeyPub(*newKeys.SigningKeyPub).
		SetVerificationKeys(newKeys.VerificationKeys).
		Exec(ctx)
	if err != nil {
		return rollback(tx, "update keys uploading: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return rollback(tx, "update keys commit: %w", err)
	}

	return nil
}
