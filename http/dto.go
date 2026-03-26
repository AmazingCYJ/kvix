package main

type entryWriteRequest struct {
	Key   string  `json:"key"`
	Value *string `json:"value"`
}

type entryDTO struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type batchWriteRequest struct {
	Entries []entryWriteRequest `json:"entries"`
}

type batchWriteResult struct {
	Count        *int `json:"count,omitempty"`
	WrittenCount *int `json:"written_count,omitempty"`
}
