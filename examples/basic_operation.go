package main

import (
	"fmt"
	"kvix"
	"kvix/common"
)

// main 展示 kvix 最基础的打开、写入、读取和删除流程。
func main() {
	opts := common.DefaultOptions
	opts.DirPath = "/tmp/kvix-data"
	db, err := kvix.Open(
		opts,
	)
	if err != nil {
		panic(err)
	}
	// 1. 写入数据。
	err = db.Put([]byte("name"), []byte("茉莉"))
	if err != nil {
		panic(err)
	}
	// 2. 读取数据。
	value, err := db.Get([]byte("name"))
	if err != nil {
		panic(err)
	}
	fmt.Printf("name: %s\n", value)

	// 3. 删除数据。
	err = db.Delete([]byte("name"))
	if err != nil {
		panic(err)
	}
	// 4. 再次读取已删除的数据，此处会返回 ErrKeyNotFound。
	value, err = db.Get([]byte("name"))
	if err != nil {
		panic(err)
	}
	fmt.Printf("name: %s\n", value)
}
