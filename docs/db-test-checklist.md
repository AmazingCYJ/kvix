# 数据库测试覆盖清单

## 1. 配置校验
- 场景: 目录为空
  - 测试: TestCheckOptions/empty_dir_path
  - 代码: checkOptions
- 场景: 数据文件大小非法
  - 测试: TestCheckOptions/invalid_data_file_size
  - 代码: checkOptions
- 场景: 合法配置
  - 测试: TestCheckOptions/valid_options
  - 代码: checkOptions

## 2. 基础 CRUD
- 场景: 空 key 写入
  - 测试: TestOpenAndBasicCRUD
  - 代码: DB.Put
  - 期望: 返回 ErrKeyNotFound
- 场景: 普通写入并读取
  - 测试: TestOpenAndBasicCRUD
  - 代码: DB.Put, DB.Get
- 场景: 同 key 覆盖写
  - 测试: TestOpenAndBasicCRUD
  - 代码: DB.Put, DB.Get
- 场景: 删除后读取
  - 测试: TestOpenAndBasicCRUD
  - 代码: DB.Delete, DB.Get
  - 期望: 返回 ErrKeyNotFound
- 场景: 删除不存在 key
  - 测试: TestOpenAndBasicCRUD
  - 代码: DB.Delete
  - 期望: 幂等，无错误
- 场景: 空 key 读取
  - 测试: TestOpenAndBasicCRUD
  - 代码: DB.Get
  - 期望: 返回 ErrKeyNotFound

## 3. 重启恢复
- 场景: 首次打开写入数据，重启后恢复索引
  - 测试: TestOpenReloadIndexFromDataFiles
  - 代码: Open, loadDataFiles, loadIndexFromDataFiles
- 场景: 删除标记重放后 key 不可见
  - 测试: TestOpenReloadIndexFromDataFiles
  - 代码: loadIndexFromDataFiles

## 4. 与测试强相关的修复点
- DB.Put 不应提前 return，否则写入逻辑不执行。
- loadDataFiles 需写回 fileIds，否则重启时不会加载索引。
- DB.Delete 追加删除记录失败时应返回真实 error。
- ReadLogRecord 在 off>=fileSize 时应返回 io.EOF，避免重启扫描尾部报头部解析错误。

## 5. 执行方式
- 仅数据库测试:
  - go test . -v
- 全量测试:
  - go test ./...
