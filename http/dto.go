package main

// entryWriteRequest 表示单条写入接口的请求体。
type entryWriteRequest struct {
	Key   string  `json:"key"`
	Value *string `json:"value"`
}

// entryDTO 表示单条 KV 记录的响应结构。
type entryDTO struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// batchWriteRequest 表示批量写入接口的请求体。
type batchWriteRequest struct {
	Entries []entryWriteRequest `json:"entries"`
}

// batchWriteResult 表示批量写入接口的结果统计。
type batchWriteResult struct {
	Count        *int `json:"count,omitempty"`
	WrittenCount *int `json:"written_count,omitempty"`
}
