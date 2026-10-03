# 实盘准备度整改进度续篇

历史记录见 [原整改进度](2026-09-24-remediation-progress.md)。此处继续原 R01–R15 范围，不代表范围缩减或真实盈利验收。

## rc955：margin 可用余额与还款金额同响应核对

- HTTP 回归先复现 margin GetBalance 因继承 spot 方法访问 /api/v3/account：margin free0.25 BTC、locked0.2 BTC时误返回 spot free999 BTC。覆盖 margin 方法，改从 /sapi/v1/margin/account 精确匹配唯一资产、解析有限非负 free；资产缺失/重复不冒充零余额，locked 不加到 free。
- 新单响应本金/利息/free能力接入真实 closeReverse：买回核账后同时验证当前本金与策略账本、含息金额不超历史净量且 free 足额，再保存精确还款请求。已花费、冻结不足额、非法读数、查询失败、取消/所有权丢失或意图保存失败不还款，历史成交仍保留。
- 可用余额不是策略资产归属：仍缺全账户其他所有者库存/支用约束、剩余资产处置、完整重启/部分成交恢复和原子世代 fencing；账户响应到 RPC 间外部资金变化不宣称原子排除。未访问真实账户/生产库、未下单或发布，不继承旧版严格验收。
- 验证：当前可用资金九种真实 closeReverse 场景与原债务刷新七种场景两轮 race 通过（66.254s），覆盖足额正例、已花费/冻结、NaN/负额、查询失败、取消/所有权及意图保存失败；最终 Binance 余额/债务 HTTP 回归两轮 race 通过（1.754s），确认正确账户、不含 locked、同响应本金/利息/free、资产缺失/重复及非法余额拒绝、明确零余额保留。最终关联策略/解码/关闭路径两轮 race 通过（250.727s）；strategy/exchange/Binance vet、diff 检查和 Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过。
- 十包 `/private/tmp/quantmesh-trading-race-rc955-final/results.json` 与 `results.md` 已读回1911 pass、8 MySQL skip、0失败、无缺包/解析错误，source_version=3.111.0-rc955、source_commit=b0029a8d、source_dirty=true；这是提交前源码回归，不是本版同提交严格 MySQL 验证。未访问真实账户/生产库、未下单或发布，不继承旧版严格验收。

## rc954：买回完成后刷新现时本金和利息

- 首轮回归夹具数量精度不匹配，先被既有超请求成交门禁拒绝，不计为债务时间差缺陷；修正到实际四位取整后，复现含息金额变化仍按旧0.4 BTC还款，以及超额利息/本金变化/查询失败或缺失组件仍进入 RPC。
- 正常 closeReverse 在净成交核账和订单结算后，通过独立负债能力重新查询同基础币本金和利息；保存请求前复核 context/owner、有限金额、策略本金一致性及该买回来源净数量。利息0.0002可按实际0.4002还款，超出净量或本金不匹配不还款。
- 生产 Binance margin 的持仓接口要求零基础币库存，不能拿买回后库存当负债证据；新增 margin/account 精确基础币负债读数及 wrapper 接线，允许已有库存但不认领它、不削弱旧持仓门禁。仍需实际可用余额/全账户归属、剩余资产、部分成交与完整重启恢复、原子世代 fencing；刷新到 RPC 间的新增利息也不能宣称原子消除。
- 验证：实际 closeReverse 七种刷新场景两轮 race 通过（30.390s），涵盖含息金额增长、净量不足、本金变化、查询错误/缺失组件、取消和所有权丢失；真实 Binance margin/account HTTP 七类场景两轮 race 通过（2.059s），覆盖买回库存、缺失/重复资产、非法/溢出金额及外币请求，并确认旧库存归属门禁不退化。最终关联策略/解码/关闭路径两轮 race 通过（215.117s）；strategy/exchange/Binance vet、diff 检查和 Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过。
- 十包 `/private/tmp/quantmesh-trading-race-rc954-final/results.json` 与 `results.md` 已读回1908 pass、8 MySQL skip、0失败、无缺包/解析错误，source_version=3.111.0-rc954、source_commit=3f8863e8、source_dirty=true；这是提交前源码回归，不是本版同提交严格 MySQL 验证。未访问真实账户/生产库、未下单或发布，不继承旧版严格验收。

## rc953：实际含息还款消耗不再绑定历史买回目标

- 回归先复现历史目标0.4 BTC、净成交0.401 BTC时，含息还款0.4002 BTC虽有足额证据仍在 RPC 前被拒绝。来源按实际请求金额检查净数量上限，确认后消耗严格绑定同账户/币种/ACK 的本金加利息事件，历史目标保持原值；不因目标差异拒绝合法含息核账。
- runtime schema6 接受历史1–5格式，旧程序不能将新消费语义静默读取；不足额、重复消费、事件不匹配继续拒绝。此批不新增自动偿债、不证明当前资产足够，当前负债/实物余额、部分成交补偿、完整恢复及原子世代 fencing 仍未闭合。
- 验证：含息消费、实际 Start ACK 恢复、利息不足额、保存失败原子回滚/重试和 schema5/6 回归两轮 race 通过（2.172s）；既有消费/来源/启动路径两轮 race 通过（14.448s，补充保存失败测试前）；最终关联策略/解码/关闭路径两轮 race 通过（186.638s）。strategy vet、diff 检查和 Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过。
- 十包 `/private/tmp/quantmesh-trading-race-rc953-final/results.json` 与 `results.md` 已读回1906 pass、8 MySQL skip、0失败、无缺包/解析错误，source_version=3.111.0-rc953、source_commit=ed061c60、source_dirty=true；这是提交前源码回归，不是本版同提交严格 MySQL 验证。未访问真实账户/生产库、未下单或发布，不继承旧版严格验收。

## rc952：重启继续已确认买回订单的逐笔成交核账

- 原 CID 接管 ACK 后同次启动继续逐笔核账；已有未核清 ACK 也进入恢复。在操作/钱包锁下重读完整账本及账户/资产身份，唯一未核清订单必须具备原 CID、价格和准备时间，通过同 ID 订单查询再次核对原请求及完成状态，再复用逐笔身份/费用/净数量门禁保存证据。
- 不重新买回、不还款、不启动交易；新单/部分成交/撤单暂保留待核账状态，部分成交后自行完成可以继续核账。缺失、重复、错订单、费用导致不足额、查询/保存失败和取消/所有权丢失不推进耐久数量。结束流程不再清除未核清成交的 in-flight，新操作不能覆盖它。
- 历史缺原请求元数据或多个未核清订单不猜测归属。净数量是历史成交证据而非当前可用资产；部分成交撤单/补偿、实际余额覆盖、当前负债/利息、剩余资产处置、自动恢复交易及原子世代 fencing 仍未闭合。
- 验证：真实 Start 净成交恢复十种场景两轮 race 通过（2.189s），覆盖完成、部分成交随后完成、成交缺失/错单/重复、扣费不足额、查询错误、所有权/取消与保存失败；关联策略/解码/关闭路径两轮 race 通过（186.798s）。strategy vet、diff 检查和 Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过。
- 十包 `/private/tmp/quantmesh-trading-race-rc952-final/results.json` 与 `results.md` 已读回1905 pass、8 MySQL skip、0失败、无缺包/解析错误，source_version=3.111.0-rc952、source_commit=67a90a1c、source_dirty=true；这是提交前源码回归，不是本版同提交严格 MySQL 验证。未访问真实账户/生产库、未下单或发布，不继承旧版严格验收。

## rc951：启动按原买回 CID 接管订单身份

- Start 在本金核账前识别买回请求，持操作锁与同账户钱包锁重新加载并验证完整账本/资产/账户，再通过 margin 的精确 CID 接口查询原请求；不扫共享订单猜测归属、不重新提交订单或还款，查询未找到保留请求。
- 核对 CID（含既定 broker 前缀）、订单 ID、币对/方向/类型、请求价格/数量、有限执行量、有效订单时间和已知状态，保存 ACK 后仍保持 UNKNOWN/in-flight，订单记录保留原价格及准备时间。新单/部分成交/撤单也只记录身份，不冒充完整成交或资产已核清。
- 查询/保存失败、取消及所有权丢失不推进耐久请求；启动恢复共用15秒 context 预算。新增请求元数据可选，历史记录不补造；完整成交、撤单补偿、余额/剩余资产核账、自动恢复交易与原子世代 fencing 仍未闭合。
- 验证：真实 Start CID 查询的21种正常/异常场景两轮 race 通过（2.205s），包括已成交/新单/部分成交/撤单、broker 前缀、查询未找到/失败、请求身份不匹配、epoch/缺失时间、所有权/取消/保存失败和错误账户/账本。关联策略/解码/关闭路径两轮 race 通过（186.725s）；strategy vet、diff 检查与 Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过。
- 十包 `/private/tmp/quantmesh-trading-race-rc951-final/results.json` 与 `results.md` 已读回1904 pass、8 MySQL skip、0失败、无缺包/解析错误，source_version=3.111.0-rc951、source_commit=4a98bb28、source_dirty=true；当前为提交前源码回归，不是本版同提交严格 MySQL 验证。未访问真实账户/生产库、未下单或发布，不继承旧版严格验收。

## rc950：买回提交前精确 CID 意图与生产透传

- 生产适配器回归先复现传入 CID 被替换：fundingCarryOrderExecutor 未透传字段，已补齐；初始隔离夹具缺锁引发的 panic 属于测试夹具问题，不计为产品缺陷，补齐依赖后实际 CID 断言仍失败，修复后两轮 race 通过。
- 买回先保存 CID、币对/资产/账户、数量、价格、目标负债和准备时间，再进入实际 RPC；ACK 与原请求数量/账户/CID（允许交易所既定 broker 前缀）匹配后，原子转换为历史订单记录。无 ACK 或错 CID 保留 in-flight/UNKNOWN，新操作不能覆盖；保存失败不提交。
- schema5 接受历史1–4格式，不补造旧请求；普通恢复拒绝待提交意图，启动本金核账保留此字段而不丢弃它。当前只提供精确恢复入口，按 CID 查询的自动接管、部分成交证据/补偿、完整资产处置及原子世代 fencing 仍未闭合。
- 验证：实际生产适配器 CID 回归两轮 race 通过（2.517s）；提交前请求/ACK/无 ACK/错 CID及保存失败两轮 race 通过（6.204s）；关联策略/解码/关闭路径两轮 race 通过（187.323s）。根包与 strategy vet、diff 检查和 Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过。
- 十包 `/private/tmp/quantmesh-trading-race-rc950-final/results.json` 与 `results.md` 已读回1903 pass、8 MySQL skip、0失败、无缺包/解析错误，source_version=3.111.0-rc950、source_commit=48ef9ec3、source_dirty=true；这是提交前源码回归，不是本版同提交严格 MySQL 验证。未访问真实账户/生产库、未下单或发布，不继承旧版严格验收。

## rc949：确认还款与买回消耗绑定

- 实际关闭链将买回订单身份写入还款意图；原有卖出拒绝/零成交/部分成交返还路径不猜测历史买回来源。提交还款前校验同账户/币种、净数量和目标偿债金额，拒绝被其他还款消耗的来源。
- 查询确认后，本金、金融事件、对应买回记录的还款 ID/消耗数量一起保存，消费按实际确认本金加利息金额而非只按本金；失败共同回滚，待还款 ACK 仍在，重试不再提交 RPC。重复确认可幂等保存关联，不重复扣减消耗。
- schema4 接受历史1/2/3格式，但不为历史无关联记录猜测来源；新格式核验消耗有限非负、不超净数量、同还款不重复分配且有匹配金融事件。订单净数量减已确认消耗只能推导账面余量，物理余额覆盖、剩余资产处置、无 ACK/部分成交和完整自动恢复仍未闭合。
- 验证：来源绑定/原子回滚/重复 RPC 拒绝两轮 race 通过（10.452s），另补真实 Start 按已保存买回来源恢复消耗两轮 race 通过（6.191s）；关联策略/解码/关闭路径两轮 race 通过（178.572s）。strategy vet、diff 检查与 Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过。
- 包含补充启动用例的最终十包 `/private/tmp/quantmesh-trading-race-rc949-final2/results.json` 与 `results.md` 已读回1900 pass、8 MySQL skip、0失败、无缺包/解析错误，source_version=3.111.0-rc949、source_commit=873f5f76、source_dirty=true；前一份 final 报告1899项未包含补充启动用例，非最终汇总。生产源码在补充测试后未再变更；当前为提交前源码回归，不是本版同提交严格 MySQL 验证。未访问真实账户/生产库、未下单或发布，不继承旧版严格验收。

## rc948：偿债买回订单与成交证据耐久记录

- 买回在成交查询前保存正订单 ACK、CID、币种/账户及请求数量/目标负债；还款前保存经现有逐笔身份、费用和覆盖核验的成交及毛/净数量。取消及丢失所有权保留本地 ACK，已失效所有者不写 durable；存储失败不继续查询或还款。
- schema3 接受历史已核清 schema1/2，新格式核验订单身份不重复、账户、逐笔费用/净数量一致，并在恢复时绑定实际基础币。快照深拷贝成交，启动本金核账不丢弃已保存买回证据。
- 本批是历史证据接线，不是已确认可支用库存；请求提交前 WAL/无 ACK、未足额或部分成交证据、还款消耗归属、剩余资产处置及自动恢复交易仍未闭合。正常关闭保留证据，不补造共享余额归属，不宣称全资产核账完成。
- 额外回归先复现本金清零后无账户标识的买回证据可被当前账户接管；将买回记录纳入账户证据检查后两轮 race 通过（6.492s）。最终关联策略/解码/关闭路径两轮 race 通过（170.547s），包括持久化失败、取消/所有权、深拷贝、净数量篡改与重复订单；strategy vet、diff 检查及 Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过。
- 账户边界修复后的十包 `/private/tmp/quantmesh-trading-race-rc948-final2/results.json` 与 `results.md` 已读回1896 pass、8 MySQL skip、0失败、无缺包/解析错误，source_version=3.111.0-rc948、source_commit=3be22a00、source_dirty=true；前一份 final 报告只覆盖账户修复前代码，不作为最终证据。当前为提交前源码回归，不是本版同提交严格 MySQL 验证；未访问真实账户/生产库，未下单或发布，不继承旧版严格验收。

## rc947：启动接入已受理还款的本金核账

- Start 原本直接拒绝待还款快照，现在在普通恢复前识别精确 ACK，持操作锁/钱包协调锁重新加载并校验 schema、账户、原借款、币种和完整本金账本，仅查询同一还款交易并保存确认本金；无 ACK 或无效归属不查询、不还款。
- 保存前复核 context/owner；历史已确认还款幂等重放。成功只清理已核清还款意图，保留 UNKNOWN/in-flight，不启动交易，不补造买回资产或订单证据。该项是本金核账的实际启动接线，不是完整自动接管，订单/资产恢复、无 ACK 归属与原子世代 fencing 仍待闭合。
- 验证：实际 Start 十种场景（正常确认、已记账重放、查询失败、错误账户、无效账本、无 ACK、错误币种、所有权丢失、取消及保存失败）两轮 race 通过；最终关联策略/解码/关闭路径两轮 race 通过（150.370s）。strategy vet、diff 检查及 Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过，并补取紧凑测试汇总。
- 十包 `/private/tmp/quantmesh-trading-race-rc947-final/results.json` 与 `results.md` 已读回1892 pass、8 MySQL skip、0失败、无缺包/解析错误，source_version=3.111.0-rc947、source_commit=58a61e8e、source_dirty=true，为提交前源码回归，不是本版同提交严格 MySQL 验证；未连接生产库/真实账户、未下单或发布，不继承旧版本严格验收。

## rc946：精确还款意图与 ACK 核账重试

- 真实关闭回归先复现还款已受理但确认查询失败时丢失精确身份并清除 in-flight 标记。
- 平仓及卖出拒绝/零成交/部分成交三条返还链均先保存精确请求，再调用还款 RPC；ACK 在查询前保存，请求包含币种、金额、目标剩余本金、账户范围和借款身份。未确认还款不清理全操作意图，不允许新操作覆盖。
- 当前操作的同 ACK 重试只查询原还款并原子保存确认本金/事件，不再次还款；未知 ID 请求拒绝盲目重发。ACK+错误、ACK 保存失败、失去所有权均保留本地身份；失效所有者不覆盖 durable。
- runtime schema2 使旧程序不能将新意图静默当作核清；新程序接受已核清 schema1，不补造旧快照的请求。未核清快照仍拒绝正常启动，自动重启恢复/无 ACK 历史归属/买回资产核账与完整接管仍待接线，不将安全暂停冒充恢复闭环。
- 验证：最终关联策略/解码/关闭路径两轮 race 通过（150.753s）；新增还款查询失败身份保留、同 ACK 不重复还款、ACK/保存/所有权故障及 schema1/2 兼容边界均通过；strategy vet、diff 检查、Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过。
- 十包 `/private/tmp/quantmesh-trading-race-rc946-final/results.json` 与 `results.md` 已读回1891 pass、8 MySQL skip、0失败、无缺包/解析错误，source_version=3.111.0-rc946、source_commit=d9e06c41、source_dirty=true，为提交前源码回归，不是本版同提交严格 MySQL 验证；不继承 rc943 严格检查点，未访问生产库/账户、未下单、未发布或验收盈利能力。

## rc945：买回扣费单位与组件一致性

- 三种真实关闭回归先复现基础币费用少报、零费用仍声明基础币扣费、已标记换算却缺失有效汇率仍被当作足额净数量并调用还款。
- 核对现有 Binance margin adapter 与 wrapper：原手续费按费用币种数量直接透传，基础币扣费来自同一原始费用。未标记转换时按原币金额校验；明确标记计价币转换时要求有效金额/正汇率并校验基础币数量，不能混用单位。正常原币扣费、计价币收费、零费用及有效换算正例保留。
- 验证：真实净偿债十二种异常/正常费用路径两轮 race 通过（50.361s），包括少报、零费用矛盾、缺失汇率及正常换算；strategy vet、diff 检查、Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过。
- 十包 `/private/tmp/quantmesh-trading-race-rc945-final/results.json` 与 `results.md` 已读回1887 pass、8 MySQL skip、0失败、无缺包/解析错误，source_version=3.111.0-rc945、source_commit=7d6ba3e2、source_dirty=true，为提交前源码回归，不是本版同提交严格 MySQL 验证；顶层用例数未增加，扩展的是实际关闭回归的子场景。
- 完整买回资产持久化/恢复、交易所原始定点精度与全账户金融核账仍待完成，不继承 rc943 严格检查点；未访问生产库/账户、未下单、未发布或验收盈利能力。

## rc944：买回逐笔费用与基础币净数量

- 五种真实反向平仓回归先复现基础币扣费后不足额、缺失成交、订单身份错误、重复成交及累计覆盖不足仍调用还款并宣称成功。
- 买回偿债前查询保证金账户同订单逐笔成交；校验订单/交易/币对/方向/时间、有限数量/费用及基础币费用身份，十进制累计成交与手续费并核对毛成交覆盖。基础币净数量不足本金加利息时不还款，禁止从共享余额补造归属证据。
- 正常夹具明确提供同订单零费用证据，不让 mock 默认 nil 成交列表冒充已核清；正常基础币收费但净额足够、计价币收费及明确零费用仍可平仓。
- 验证：最终关联策略/解码/关闭路径两轮 race 通过（126.688s），覆盖五类不完整/不足额成交及三类正常费用路径，并保留已有本金/最终所有权/关闭检查点回归；strategy vet、diff 检查及 Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过。
- 十包 `/private/tmp/quantmesh-trading-race-rc944-final/results.json` 与 `results.md` 已读回1887 pass、8 MySQL skip、0失败、无缺包/解析错误；source_version=3.111.0-rc944、source_commit=d8c7ced3、source_dirty=true，为提交前源码回归，不是本版同提交严格 MySQL 验证。
- 该步骤证明成交账本推导的净数量，不是完整账户余额/资产归属核账；买回资产的耐久记录、失败后恢复与剩余资产处置仍未闭合。不继承 rc943 同提交严格验证，未访问生产库/账户、未下单、未发布或验收盈利能力。

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
