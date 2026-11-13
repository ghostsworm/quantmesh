# 交接：网页网格回放改读已校验的本地行情

- 日期：2026-11-13
- 分支：`feat/daily-latest-panel`，已推送，与 `origin/feat/daily-latest-panel` 一致
- 分支尖：`2c359e9d`（修复（回测）：缺行情时不占用优化名额）
- 版本：`3.112.0-rc11`（开发中，未发布）
- 基线：`origin/main` 之后的 9 个提交，从 `151b349c` 到 `2c359e9d`
- PR：未开。本机 `gh` 登录的 `xurenlu` 对 `ghostsworm/quantmesh` 没有写权限，只能由有权限的人手动开。

这份文档记录接手时的事实。设计和决策以 [replay ADR](../decisions/2026-09-17-replay-backtest.md) 和 [日账 ADR](../decisions/2026-11-05-daily-equity-reconciliation.md) 为准。

## 已经做完

| 版本 | 提交 | 做了什么 |
|---|---|---|
| `3.112.0-rc3` | `151b349c` | 盈利页只读展示最近一个已闭合自然日的日账。没有报告显示未发布，不改变汇总核实标志，也不改变提现权限 |
| `3.112.0-rc4` | `c451ca13` | 带 `replay_walk_forward` 的优化任务只读本地已校验的 data.binance.vision U 本位合约 1 分钟文件。缺一天、校验和不符或同一天同时有 zip 与 csv，整次失败，不回退到交易所，也不补造 K 线 |
| `3.112.0-rc5` | `e40a3c4b` | 新增 `go run ./tools/visionfetch`，单独下载这些文件和官方校验文件。校验失败或被重定向，本次下载的全部新文件都会删除，已有文件不覆盖。回放任务本身不下载 |
| `3.112.0-rc6` | `d12c0217` | 每个合约日文件必须是完整的 1440 根 1 分钟 K 线。下载命令和回放读取用同一个 `visionfeed.CompleteUTCDay`。资金费文件不按分钟要求 |
| `3.112.0-rc7` | `85f10f24` | 缺行情的回放任务在入库前就失败，不留下任务 |
| `3.112.0-rc8` | `9c751d5e` | 窗口覆盖的月份只要有一个资金费文件，其余月份也必须有。缺一个月整次失败，不能用别的月份顶上。这些月份一个文件都没有时，仍沿用填写的固定费率 |
| `3.112.0-rc9` | `eec97378` | zip 里只接受唯一一个 csv，条目名必须是把 zip 文件名的 `.zip` 换成 `.csv`。带目录、`..` 或另有第二个 csv，整次失败。1 分钟日文件不超过 2 MiB，一个月的资金费不超过 256 KiB |
| `3.112.0-rc10` | `165f51f5` | 回放任务创建失败或运行失败时，页面上的原因不再包含行情目录。目录只写在服务端日志 |
| `3.112.0-rc11` | `2c359e9d` | 先读本地已校验行情，再占唯一的运行名额。读不到就不占名额，不会把下一个任务挡成「已有任务运行中」 |

普通回测任务和不带 replay 的优化任务仍走原来的未校验缓存和交易所下载，这一轮没有改。

## 没有做完

1. **没有开 PR。** 标题用「新增（回测）：网格回放改读已校验的本地行情」。合进 `main` 只会跑 `.github/workflows/ci.yml`，不会打 tag，也不会部署。发布只在推送 `v*` tag 时跑 `.github/workflows/cd.yml`。
2. **没有跑全仓测试。** 本地只跑过 `go test ./backtest/visionfeed/ ./backtest/visionfetch/ ./backtest/optimrun/ ./tools/visionfetch/`、`go vet`，以及网页回放的 Ruby 验收测试 `go test ./web/ -run '^TestReplayOptimizerAPIWithRuby$'`。PR 上的 CI 会第一次跑全仓和 MySQL。
3. **没有连接真实账户，也没有对 data.binance.vision 做真实下载。** 下载命令的测试全部用本地伪造的响应。命令退出 0 只表示请求范围内的文件都已校验，不表示回放已经跑过，也不代表盈利。退出 2 是参数错误，3 是有文件下载失败，4 是整次请求失败。
4. **没有打 tag，也没有部署。** 生产服务器跑的是更早的构建。不要为了这一轮推送 `v*` tag。

## 不要动

- `web/api_profit.go` 里的 `equityReconciliationComplete` 仍是 `false`。单日通过不能把 lifetime/today/week/month 的核实标志改成通过，也不能解锁提现。
- 合约证据是候选采样，不是历史日终端快照。现货和全仓杠杆没有账本型的只读来源，只有合约的一天保持未核实，原因码要明说。不要补造钱包，不要加日账定时任务，也不要新写一个 Binance 现货或全仓杠杆的只读来源。
- 不要连接或写入生产数据库。`facev.app` 用的是 `/opt/quantmesh/data/quantmesh.db`，把它拷回来跑有可能对同一个账户重复下单。家用机 `q3` 用 MariaDB。两台都跑着更早的构建。
- 回放任务不下载，也不回退到交易所接口。下载命令不跟随重定向，不覆盖已有文件，任何一个文件失败就删除本次新建的全部文件。
- 不要提交 `--help/` 和 `.codex/`。这两个目录是未跟踪的本地文件。

## 接手时还在的缺口

1. **创建任务时的那次读取不能取消。** `backtest/optimrun/vision_loader.go` 的 `preflightReplayData` 用 `context.Background()`。请求断开或服务端写超时都停不掉它。rc11 之后它不再占运行名额，但一个很大的窗口仍会占着这一次请求，直到读完。要改的话，用请求自己的 context，并加上限。
2. **同一个窗口会读两次。** 创建时读一次，任务开跑时再读一次。两次之间文件被换掉，任务会按第二次的结果失败或成功。这是故意留下的：第二次读取才是任务实际用的行情。
3. **下载命令的响应体上限是 512 MiB。** `backtest/visionfetch/fetch.go` 用 `io.LimitReader` 截断。超过这个大小会因为校验和对不上而失败，不会落地。没有再改成「超过上限就立刻报错」，因为它不改变已经落地的回放结果。这一轮用的 Go 1.26.3 里，关闭响应体不会把没读完的字节读完，所以这个上限不会把剩下的响应体拖完。

## 验证时要先知道的

- 这个仓库的 commit message 用繁体中文，见 `AGENTS.md`。提交身份读本地 git config，经常换，提交前先读 `git config --local user.name` 和 `user.email`。
- Go 文件用制表符缩进。按文本替换时要用文件里的真实制表符，改完跑 `gofmt`。
- 测试文件只放明显的假凭据。会话环境会把像密钥的字面量替换成占位符，所以不要靠读文件来核对已提交的字面量。
- 行情目录缺省是 `data/kline/binance_vision`，配置项是 `backtest_data.binance_vision_dir`，下载命令用 `-root` 指定。`data/` 已在 `.gitignore` 里。
- 每次改代码都要同步版本号，只递增 rc：`internal/app/main.go` 的 `Version`、`webui/package.json`、`ARCHITECTURE.md`、`PRODUCT_OVERVIEW.md`（开头和「最后更新」）、`product-overview.md`、`CHANGELOG.md`，再加上对应的决策文档。这一份交接文档不改代码，所以版本号仍是 `3.112.0-rc11`。
