package main

import (
	bitcask "bitcask-my"
	"bitcask-my/common"
	"fmt"
)

func main() {
	opts := common.DefaultOptions
	opts.DirPath = "/tmp/bitcask-data"
	db, err := bitcask.Open(
		opts,
	)
	if err != nil {
		panic(err)
	}
	// 1.写入数据
	err = db.Put([]byte("name"), []byte("茉莉"))
	if err != nil {
		panic(err)
	}
	// 2.读取数据
	value, err := db.Get([]byte("name"))
	if err != nil {
		panic(err)
	}
	fmt.Printf("name: %s\n", value)

	// 3.删除数据
	err = db.Delete([]byte("name"))
	if err != nil {
		panic(err)
	}
	// 4.尝试读取已删除的数据，应该返回 ErrKeyNotFound 错误
	value, err = db.Get([]byte("name"))
	if err != nil {
		panic(err)
	}
	fmt.Printf("name: %s\n", value)

}
