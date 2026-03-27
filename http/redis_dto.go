package main

// redisKeyOnlyRequest 供只需要逻辑 key 的 Redis HTTP 命令复用。
// 例如 get、del、ttl 这类命令都只需要调用方提供一个 key。
type redisKeyOnlyRequest struct {
	Key string `json:"key"`
}

// redisStringSetRequest 表示 string/set 命令的请求体。
// 如果调用方同时传入 ttl_seconds，就在写入字符串时顺带设置过期时间。
type redisStringSetRequest struct {
	Key        string  `json:"key"`
	Value      *string `json:"value"`
	TTLSeconds *int64  `json:"ttl_seconds,omitempty"`
}

// redisStringExpireRequest 表示 string/expire 命令的请求体。
// 这里单独拆出结构，是为了让“设置值”和“修改 TTL”两个命令的语义更直观。
type redisStringExpireRequest struct {
	Key        string `json:"key"`
	TTLSeconds *int64 `json:"ttl_seconds"`
}

// redisFieldOnlyRequest 供只需要 key + field 的 hash 命令复用。
// 例如 hget、hdel、hexists 都属于这一类。
type redisFieldOnlyRequest struct {
	Key   string `json:"key"`
	Field string `json:"field"`
}

// redisHashSetRequest 表示 hash/hset 命令的请求体。
// 它要求调用方同时给出 key、field 和对应的 value。
type redisHashSetRequest struct {
	Key   string  `json:"key"`
	Field string  `json:"field"`
	Value *string `json:"value"`
}

// redisValuesRequest 供一批字符串值写入 list 的命令复用。
// 例如 lpush 和 rpush 都会一次接收多个 value。
type redisValuesRequest struct {
	Key    string   `json:"key"`
	Values []string `json:"values"`
}

// redisMemberOnlyRequest 供只需要 key + 单个 member 的命令复用。
// 例如 sismember、zscore、zrem 都会走这个结构。
type redisMemberOnlyRequest struct {
	Key    string `json:"key"`
	Member string `json:"member"`
}

// redisMembersRequest 供需要一组 member 的集合命令复用。
// 例如 sadd、srem 都需要一次接收多个 member。
type redisMembersRequest struct {
	Key     string   `json:"key"`
	Members []string `json:"members"`
}

// redisZSetAddRequest 表示 zset/zadd 命令的请求体。
// score 使用指针是为了能区分“没传 score”和“传了 0”。
type redisZSetAddRequest struct {
	Key    string   `json:"key"`
	Member string   `json:"member"`
	Score  *float64 `json:"score"`
}

// redisRangeRequest 表示需要 start/stop 范围参数的命令请求体。
// 目前 list/lrange 会复用这个结构。
type redisRangeRequest struct {
	Key   string `json:"key"`
	Start *int64 `json:"start"`
	Stop  *int64 `json:"stop"`
}

// redisAddedResult 表示“是否新增”的返回值。
// hset/zadd 这类命令会用它告诉调用方这次是不是首次插入。
type redisAddedResult struct {
	Added bool `json:"added"`
}

// redisDeletedResult 表示删除类命令的返回值。
// deleted=true 代表本次请求确实删除了一个现存 key。
type redisDeletedResult struct {
	Deleted bool `json:"deleted"`
}

// redisUpdatedResult 表示更新类命令的返回值。
// updated=true 代表底层确实找到了目标 key，并完成了本次更新。
type redisUpdatedResult struct {
	Updated bool `json:"updated"`
}

// redisExistsResult 表示存在性判断类命令的返回值。
type redisExistsResult struct {
	Exists bool `json:"exists"`
}

// redisCountResult 表示计数类命令的返回值。
// 底层很多 Redis 结构都会返回元素数量，因此统一抽成 count。
type redisCountResult struct {
	Count uint32 `json:"count"`
}

// redisTTLResult 表示 TTL 类命令的返回值。
// ttl_seconds=-1 表示 key 存在，但当前没有设置过期时间。
type redisTTLResult struct {
	TTLSeconds int64 `json:"ttl_seconds"`
}

// redisHashValueResult 表示 hash 读取命令的返回值。
// 返回 key 和 field 是为了让 HTTP 调用方在日志里更容易对齐本次命令上下文。
type redisHashValueResult struct {
	Key   string `json:"key"`
	Field string `json:"field"`
	Value string `json:"value"`
}

// redisValueResult 表示只返回一个 value 的命令结果。
// list 的 lpop/rpop 会用这个结构。
type redisValueResult struct {
	Value string `json:"value"`
}

// redisValuesResult 表示返回字符串数组的命令结果。
// list/lrange 等命令会用它把有序结果返回给调用方。
type redisValuesResult struct {
	Values []string `json:"values"`
}

// redisMembersResult 表示返回成员数组的命令结果。
// set/zset 会用它把成员列表暴露给 HTTP 调用方。
type redisMembersResult struct {
	Members []string `json:"members"`
}

// redisScoreResult 表示 zscore 命令的结果。
type redisScoreResult struct {
	Score float64 `json:"score"`
}

// stringsToByteSlices 把 HTTP 层的 []string 转成底层 RedisDataStore 需要的 [][]byte。
func stringsToByteSlices(values []string) [][]byte {
	out := make([][]byte, 0, len(values))
	for _, value := range values {
		out = append(out, []byte(value))
	}
	return out
}

// byteSlicesToStrings 把底层返回的 [][]byte 转回更适合 JSON 输出的 []string。
func byteSlicesToStrings(values [][]byte) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, string(value))
	}
	return out
}
