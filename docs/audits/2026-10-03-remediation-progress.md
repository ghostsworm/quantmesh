# 实盘准备度整改进度续篇

历史记录见 [原整改进度](2026-09-24-remediation-progress.md)。此处继续原 R01–R15 范围，不代表范围缩减或真实盈利验收。

## rc943：同提交严格 MySQL 验证检查点

- 验证源码提交：`e19a71836eb0428d4e7d145ad36c9756e6373774`，版本 `3.111.0-rc943`。测试前后 HEAD 相同，`git diff --exit-code` 与 `git diff --cached --exit-code` 均通过；未修改源码。报告 `source_dirty=true` 仅因原有未跟踪 `--help/types.json`、`--help/types.md`，不宣称全工作树干净，不移动/提交这些无关文件。
- 命令：`ruby scripts/verify_trading_race.rb /private/tmp/quantmesh-trading-race-rc943-same-commit-mysql --require-mysql`，只向本次创建的一次性本地测试库设置 MySQL DSN 和 destructive-schema 显式测试许可，没有使用生产环境配置。
- 环境：MySQL `8.0.36`，镜像 `sha256:a532724022429812ec797c285c1b540a644c15e248579c6bfdf12a8fbaab4964`；容器 `quantmesh-readiness-mysql-rc943`，ID `a9db4df7c8799511a5769e307923ba75072726ad272c80df6540676d7c9955a3`。启动/清理前核对身份与 `isolated-readiness-test` 标签，无宿主机数据卷，`/var/lib/mysql` 为512MiB tmpfs，内存1GiB/CPU2，只监听 `127.0.0.1:32777`。
- `results.json`、`results.md` 已读回：1894 pass、0 skip、0 fail、无缺包/解析错误，`mysql_required=true`、`missing_verified_mysql_cases=[]`；强制八项包括实例开仓暂停、资金费覆盖、钱包预留、借贷利息账本、订单成交覆盖、现货库存迁移、歧义归属回填拒绝、利润提现规则，均有独立 pass 终态。
- 同一源码提交的 `yarn verify` 类型检查、44文件224测试及 Vite/PWA 构建通过；strategy vet、diff 检查、Ruby 门禁9项/58断言通过。没有修改源码后沿用此次验收；后续源码变更需要重新验证。
- 测试完成后删除上述精确容器并读回确认不存在；一次性 tmpfs 测试数据不可恢复，镜像和 JSON/Markdown 报告保留。此检查点仅证明本地交易核心回归及八项 MySQL 验证，不证明完整恢复补偿、交易所净到账/全账户核账、多实例原子 fencing、部署或真实盈利。

## rc943：买回本金与利息的成交核验

- 四种真实关闭回归先复现：只覆盖本金、NaN/无穷大成交、超过委托数量的成交仍调用还款并返回成功；另一个回归复现买回后失去所有权仍调用还款。
- 买回委托取整后不能少于总负债；成交必须有限、非负、不超过委托量且足够覆盖本金与利息，只允许有限机器精度误差，不使用交易数量容差。还款前复核 context/owner。
- 两处旧正例夹具返回超过其委托量的成交，改成实际可成交范围，而非放宽超量门禁。
- 验证：最终关联策略/解码/关闭路径两轮 race 通过（94.326s），覆盖四类非法/不足额成交、失去所有权不还款、正常含息还清与关闭检查点；strategy vet、diff 检查及 Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过。
- 十包 `/private/tmp/quantmesh-trading-race-rc943-final/results.json` 与 `results.md` 读回1886 pass、8 MySQL skip、0失败、无缺包/解析错误；source_version=3.111.0-rc943、source_commit=45913e8c、source_dirty=true，为提交前源码回归，不是同提交严格 MySQL 验收，不继承旧版验收。
- 毛成交数量仍不是扣费后净到账，手续费/买回资产归属/余额覆盖、剩余资产与接管补偿仍待闭合；未访问生产库/账户、未下单、未发布或验收盈利能力。

## rc942：期货已关闭的耐久检查点

- 三组真实反向平仓回归先复现：已平期货在后续买回失败时仍保存旧数量、检查点保存失败/所有权丢失后仍调用买回接口。
- 交易所期货快照须确实为零；持策略锁复核 context/owner、方向、原数量与 intent，再保存零期货数量，但保留债务、借款身份及未完成意图。保存失败回滚数量、标记 UNKNOWN，不继续买回/还款。
- 验证：最终关联策略/解码/关闭路径两轮 race 通过（74.682s），覆盖检查点后买回失败、保存失败/数量回滚、失去所有权不覆写 durable、微量期货残余拒绝及完整两腿正常关闭可恢复解码。strategy vet、diff 检查及 Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过。
- 十包 `/private/tmp/quantmesh-trading-race-rc942-final/results.json` 和 `results.md` 读回1884 pass、8 MySQL skip、0失败、无缺包/解析错误；source_version=3.111.0-rc942、source_commit=917060c8、source_dirty=true，为提交前源码回归，不是同提交严格 MySQL 验收，不继承旧版验收。
- 已平仓但持久化失败的恢复、微量期货处置、买回资产核账和完整接管补偿仍待闭合；未访问生产库/账户、未下单、未发布或验收盈利能力。

## rc941：反向平仓实际本金与残债核验

- 三种真实关闭回归先复现部分还本、微量本金、微量还款后负债仍返回成功并清空债务/借款身份。
- 平仓使用确认本金扣减；本金/利息核对采用金额一致性，不以交易数量容差跳过微量债务或接受不匹配账目。还款后负债及本金/利息组件必须为零；最终清除身份前复核 context/owner。
- 小于交易数量精度的欠款不能直接清零，暂拒绝零数量买单并保留身份。该项不是完整微量资产处置方案，完整恢复/接管与自动补偿仍未闭合。
- 验证：相关策略/解码/关闭路径两轮 race 通过（62.657s）；最终五种欠款异常、最终所有权丢失及正常完整借还账本恢复另两轮 race 通过（22.188s）。strategy vet、diff 检查及 Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试和 Vite/PWA 构建通过。
- 十包 `/private/tmp/quantmesh-trading-race-rc941-final/results.json` 与 `results.md` 读回1882 pass、8 MySQL skip、0失败、无缺包/解析错误，source_version=3.111.0-rc941、source_commit=c1f2e3b1、source_dirty=true；这是提交前源码回归，不是同提交严格 MySQL 验收，不继承旧版验收。
- 未访问生产账户/数据库、未下单、未发布或验收真实盈利能力。还款 RPC 已受理但确认失败的完整恢复、已买入剩余资产核账与接管补偿仍待完成；安全拒绝不等于恢复闭环。

## rc940：借款调用中断的身份与待恢复意图

- 四种真实反向开仓隔离回归先复现 ACK 身份丢失：调用取消、运行所有权丢失、正 ID 同时返回错误、取消后保存失败。
- 正 ID 先保留本地；当前所有权仍有效时保存恢复元数据，取消后标记 UNKNOWN；已失效所有者不覆盖 durable。带错误 ACK 不查询、卖出、对冲或还款，不冒充已核实本金事件。
- 未核实借款时普通错误返回不再清除 in-flight 意图；只有债务确认并保存后才进入原有结束流程。
- 验证：相关 Funding Carry / 解码 / 关闭路径两轮 race 通过（50.578s）；最终新增断言及缺失身份/意图测试另两轮 race 通过；strategy vet、diff 检查、Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过。
- 十包报告 `/private/tmp/quantmesh-trading-race-rc940-final/results.json` 与 `results.md` 读回 1880 pass、8 MySQL skip、0失败、无缺包/解析错误；版本 rc940，source_commit=4817e36b、source_dirty=true。这是提交前源码回归，不是同提交严格 MySQL 验收，不能继承 rc926 验收。
- 原子世代 fencing、ACK 保存失败后的完整耐久接管、自动核账补偿仍未闭合；取消 context 下保存元数据不是重新获得钱包 lease。未连接真实账户或生产库，未下单、发布或验收盈利能力。
