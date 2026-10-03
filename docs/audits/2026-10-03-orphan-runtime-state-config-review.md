# 无钱包预留的金融状态：恢复配置保护复查

基线：`7218cb432a03fd6dd85e80327a6ba6cdea0a88eb`，版本 `3.111.0-rc968`。这是 R09/R07 的新增缺陷证据，不是修复、发布或盈利验收。上一轮代码修改为进展；本轮增加了真实数据库复现和下一步约束，不以状态重述替代工作。

## 当前证据

rc968 保护整份配置文件更新/删除的受管实例及钱包预留，现有Bot/组删除也检查预留；这些入口都未检查 `strategy_runtime_states`。本轮没有假定真实账户存在此状态，使用隔离临时SQLite种入两类独立耐久金融记录，并确认钱包预留查询为false。

- `pending`：schema6，已知归属，0.4现货/合约历史敞口，intent/exposure标记未核清。真实恢复解码器接受恢复模式、严格模式拒绝未完成状态。
- `remaining`：schema6，已确认逐笔买回0.4008 BTC、还款消耗0.4 BTC，历史差额0.0008 BTC；intent/exposure标志已清零、direction=0。真实恢复解码器接受恢复模式，严格模式仍拒绝历史剩余量。该量不是现时可交易/可转出余额。

两项夹具兼容验证均通过（race 1.764s）。初轮填充成交对象时误用snake_case；实际OrderFill没有JSON字段标签，真实解码器识别出无效成交证据。已修正为实际Go字段序列化，重新验证夹具后重跑全部八项API红测，未采用初轮无效剩余资产输入。

## 实际数据库读回

以下结果分别在两类输入上复现，共八项安全断言失败。所有接口均200，耐久金融payload均原样保留，但恢复配置被更换或移除；不是金融payload本身被删除。

| 入口 | 配置文档 | 主配置身份 | 两类状态结果 |
|---|---|---|---|
| PUT `/api/bots/:id/config-file` | 覆盖为ETHUSDT/futures | 原BTCUSDT/funding_carry身份被更换 | 200，未阻止 |
| DELETE `/api/bots/:id/config-file` | 文档删除 | 主配置fallback仍在 | 200，部分恢复资料丢失 |
| DELETE `/api/bots/:id` | 文档删除 | Bot被移除 | 200，未阻止 |
| DELETE `/api/bot-groups/:id` | 成员文档删除 | 成员Bot被移除 | 200，未阻止 |

实际运行临时SQL存储和handler，生命周期提供者为隔离夹具；不是完整默认管理器HTTP或浏览器E2E，也不证明生产账户已经遇到该状态。八项Web测试退出1是当前开放缺陷证据，不能写成“测试通过”。

## 修复必须覆盖的范围

1. 在生命周期保护内核查同Bot的所有耐久金融记录，不能仅按当前配置中的策略列表查找，避免配置变更后漏查旧策略。需要支持context的Bot维度存储读接口；现有按策略跨Bot列表不能替代完整、受控的同Bot读取。
2. 用各策略实际schema/恢复验证逻辑判断未完成意图、敞口、负债、剩余资产和证据完整性；不能依靠通用JSON布尔值/数量为零，更不能把读取失败、未知schema或损坏记录当作空状态。
3. 核验必须有明确的“已知无记录/可安全修改”“存在待核账内容”“无法核实”区别。正常核清历史记录不能一律永久封锁；配置安全性核验本身也不能冒充交易所实时空仓证明。
4. 一致覆盖上述四个入口，再审查策略参数、整份全局配置、热更新/历史恢复等其他写入口，不能用只修config-file宣称全配置保护完成。
5. 将本轮八项红测整理成仓库回归，增加已核清/无记录正常放行、读取失败、错Bot/错symbol/错账户、未知schema和损坏数据测试；旧策略记录也必须核查。必须保留配置文档、主身份和原金融payload的直接读回。
6. 本地生命周期锁与数据库查询快照不等于跨进程原子fencing；双配置文档写入/删除的原子性也是独立未完成事项，不能由这些测试继承证明。

这轮只证明Funding Carry两类有效状态的四入口缺陷；DCA、信号、Martingale、Combo、双永续及现货借还款等schema仍需相应证据，不推定已经验收。R01–R15范围保持，完整失败实例接管、部分成交/UNKNOWN恢复、当前库存归属/处置、目标平台交付与净盈利证据仍未闭合。

## 重跑材料

JSON/Markdown、两个金融payload、两个Go overlay测试及映射位于 `/private/tmp/quantmesh-orphan-state-audit.SuqaY2/`；临时目录可能被系统清理，永久回归应复制并校验所选夹具，而不是依赖临时文件永远存在。

```sh
go test -race -overlay=/private/tmp/quantmesh-orphan-state-audit.SuqaY2/overlay.json ./strategy -run '^TestAuditOrphan' -count=1 -v
go test -race -overlay=/private/tmp/quantmesh-orphan-state-audit.SuqaY2/overlay.json ./web -run '^TestAuditOrphan' -count=1 -v
```

第一项应通过；当前基线第二项应有八项叶子安全断言失败。没有触碰真实账户/生产库、改业务源码、递增版本、改main、打tag、推送、发布或部署。
