# RC1162 测试计划

## A. 生产组合验收

使用唯一临时 SQLite 文件及假的管理员身份/订单运行态；测试必须穿过真实 service composition builder 和受保护路由。断言：路由身份校验确实执行；创建 case 与 audit 同步 durable；列表读回匹配 owner 和版本；重建 `StorageService` 与 production service 后读回一致；`Accounting`/`Gate` 缺失时执行/释放不返回成功、不改 case 状态、不写成功审计。当前公开 API 没有单案详情路由，不以未实现的接口作为验收条件。

## B. SQLite 迁移并发

多个独立连接/StorageService 以 barrier 同时迁移同一旧版隔离 schema 和空 schema，并并发重复迁移。验证无 duplicate-column 泄漏、hang、部分 schema、重复 audit/schema 对象；同名但类型、空值属性或默认值不兼容的 revision 列必须被拒绝；逐项检查所需列/index/table。

## C. MySQL 迁移并发

disposable MySQL 8 schema 强制运行：从部分旧版 reconciliation schema 并发升级、并发冷启动新 schema、并发重复初始化，并拒绝同名但长度不兼容的列。验收检查 `information_schema` 精确列定义与索引，且测试必须使用唯一隔离库/对象名并通过仓库 destructive opt-in 防护。

## D. 回归与证据

跑 Go 定向包及 race；`go test ./...`；强制 MySQL integration/race gate；`go vet ./...`。若无本机 DSN，应记录测试被跳过，并以同 SHA CI MySQL job 作为远端证据，不能把 skip 记作通过。确认前后端版本一致、CHANGELOG/overview 更新，工作区 untracked 未改变。没有 real-account、交易、发布或盈利验收。
