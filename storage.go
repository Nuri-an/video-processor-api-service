package main

import (
	"io"
	"os"
	"path/filepath"
)

type Storage interface {
	Init() error
	Put(key string, source io.Reader) error
}

type LocalStorage struct{ Root string }

func (s LocalStorage) Init() error { return os.MkdirAll(s.Root, 0755) }

func (s LocalStorage) Put(key string, source io.Reader) error {
	path := filepath.Join(s.Root, filepath.Base(key))
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, source)
	return err
}