package store

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"robot/apps/local-agent/internal/secure"
)

type Store struct{ db *sql.DB }

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "agent.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL;
CREATE TABLE IF NOT EXISTS settings (name TEXT PRIMARY KEY, value BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS identity (id INTEGER PRIMARY KEY CHECK(id=1), instance_id TEXT NOT NULL);`)
	if err != nil {
		db.Close()
		return nil, err
	}
	b := make([]byte, 16)
	if _, err = rand.Read(b); err != nil {
		db.Close()
		return nil, err
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	id := fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
	if _, err = db.Exec("INSERT OR IGNORE INTO identity(id,instance_id) VALUES(1,?)", id); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) InstanceID() (string, error) {
	var id string
	err := s.db.QueryRow("SELECT instance_id FROM identity WHERE id=1").Scan(&id)
	return id, err
}
func (s *Store) Get(name string) ([]byte, error) {
	var sealed []byte
	err := s.db.QueryRow("SELECT value FROM settings WHERE name=?", name).Scan(&sealed)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return secure.Unprotect(sealed)
}
func (s *Store) Put(name string, plain []byte) error {
	sealed, err := secure.Protect(plain)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO settings(name,value) VALUES(?,?) ON CONFLICT(name) DO UPDATE SET value=excluded.value", name, sealed)
	return err
}
func (s *Store) Delete(name string) error {
	_, err := s.db.Exec("DELETE FROM settings WHERE name=?", name)
	return err
}
func (s *Store) ReplaceConfig(plain []byte) error {
	sealed, err := secure.Protect(plain)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT INTO settings(name,value) VALUES('config',?) ON CONFLICT(name) DO UPDATE SET value=excluded.value", sealed); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM settings WHERE name='form'"); err != nil {
		return err
	}
	return tx.Commit()
}
