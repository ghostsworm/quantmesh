# 实盘准备度整改进度续篇

历史记录见 [原整改进度](2026-09-24-remediation-progress.md)。此处继续原 R01–R15 范围，不代表范围缩减或真实盈利验收。

## rc975：实际运行时热更新不跳过专用回调、不在风控失败前发布参数（本地验证完成）

- 在75532c41基线实际BotManager/SPM隔离overlay复现两红测：无SPM专用实例的UpdateOpenControl调用数0、未触发失败封锁；注入无限verifiedCapitalBudget使网格风控拒绝时，管理器旧interval100仍在，但SPM/内层已变200、order_quantity250且返回已更新ID。该预算是故障注入，不宣称线上实际预算无限；其invalid-budget gate已封锁，不据此声称真实违规订单发生。正常网格控制通过。原根包红测退出1、1.178s。
- 提取实际更新函数至bot_manager_runtime_params.go，避免继续扩大已超过2000行的管理器文件。逐实例在configMu内先调用真实publishRiskControlsLocked；失败恢复原管理器配置并立即退出，不继续更新SPM/内层，也不列入成功IDs。无SPM但有UpdateOpenControl的专用实例实际调用回调，失败保留旧配置/封锁，成功重试发布新控制并仅清除自身失败封锁。内层配置从已限额的最终管理器配置生成，而非未经预算夹紧的输入。
- 新永久回归使用实际管理器、实际SPM及受控专用回调，未连接交易所或提交订单：失败拒绝、各层旧参数、专用成功重试、正常网格均通过。定向原风控/新热更新三轮14.409s；增加重试后新三项三轮1.823s。最终根包完整race24.489s、Web完整race93.415s、vet/diff均终态通过；未将旧版策略全包/严格MySQL/远端CI当本版证据。
- Yarn verify整链终态57.90s，45文件225项；Ruby嵌入14runs27assertions/交易9runs58assertions零失败/错误/跳过。Go+React嵌入技能用于本地Make构建及compiled embed1.086s，通过；临时二进制--version为3.111.0-rc975，API版本接线保持。两清单SHA256 `6f80f5bf2397e559165decde471b0c5bb048cc33757671f5f6a51854be6c8908`，旧嵌入备份 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-3151-k54bz9/dist` 保留。隔离产物/JSON/Markdown读回目录 `/private/tmp/quantmesh-runtime-apply-rc975.OMqeVn/`，提交后额外二进制重建的实际SHA与摘要以报告为准；不覆盖运行程序。
- 限制：旧接口仅返回成功IDs，失败原因仍未逐Bot回传给HTTP；配置保存、风控回调与多实例应用不是共同事务，专用回调自身部分应用的补偿也未证明。更新成功IDs表示实际应用成功，不限价格字段改变；不是配置有记录就称应用成功。其他全局写入口/热更新的生命周期协调、实时金融核账、旧日志身份迁移与跨进程fencing仍开放。下一步应提供结构化逐Bot结果并将保存/应用状态明确反映到API，不能仅靠日志或空IDs让调用方猜测。
- R01–R15完整目标保持；未改main/tag、推送、部署、生产数据或真实账户，目的地批准仍未收到。没有真实实盘或盈利验收，也没有默认浏览器端到端应用证明。

## rc974：策略写入生命周期保护与快照冲突核验（本地验证完成）

- 实際BotManager/适配器新增策略写入协调：复用同一生命周期锁和shutdown admission，回调得到注册表managed事实，不因受管但不交易而当作已停止。旧整份配置门禁仍拒绝受管实例。策略API在该锁内读最新配置；策略列表（含参数/权重）、方向、库存策略发生变化时，受管实例拒绝409；非受管实例核验钱包预留及全部金融日志，未核清409/不足证据503。不会自动Stop或清除原账本。
- 智能挂单等既有非契约字段仍可写入/派发热更新。永久HTTP/SQLite夹具确认pending账本受管时智能挂单参数200、配置读回与updater派发、原金融payload不变；这是fixture派发证明，不是默认运行时应用成功或收益证明。策略定义/权重原先仅持久化而不完整热重构，本轮要求停止后变更，不能将其统称正常热调整。
- 写入使用配置锁内完整快照比较：核验期间配置变化则409，不覆盖新配置；保留原put_bot_strategy历史source，持久化后才派发热更新。此为单进程CAS，不证明数据库跨进程fencing或配置/金融日志/多文档共同事务。旧核清日志更换身份后的迁移仍未实现，不能把允许修改称作下一次启动恢复必然成功。
- 原SpotShort借款group/symbol两红测均409、身份不变、日志原样、仍Required；永久回归覆盖正常flat/absent、未知scope/旧策略、缺协调器、受管契约拒绝、真实管理器注册状态、并发配置变化，以及取消/存储失败不更新内存或派发。根包原生命周期启动排他回归由同一新底层实现继续覆盖；尚未构成默认HTTP到完整StartBot/浏览器E2E。
- 首轮新增热更新测试误在通用Storage接口调用可选读取方法而编译失败，改用实际SQLStorage；并发测试最初注入会被Validate归一化还原的legacy交易字段，未制造真实配置变化而失败，改为可持久化Name并读回后通过。没有隐藏失败或将原无效并发夹具算证据。
- 最终生产源码完整race根包22.496s、Web75.569s、配置2.809s完成通过，vet/diff通过；前一轮定向含全部原四入口及新增策略/生命周期三轮根包2.303s、Web42.110s通过，后来新增持久化/取消测试再次全部策略写回归三轮11.767s通过。Yarn verify45文件225项、Ruby嵌入14runs27assertions/交易9runs58assertions均通过；没有继承rc973的策略全包结果作本版验收。
- Go+React嵌入技能用于本地Make构建与compiled embed1.049s，之后在不变前端清单上重建最终后端；版本rc974、API版本接线保持。产物 `/private/tmp/quantmesh-strategy-guard-rc974.yntWib/quantmesh` SHA256 `0207722ebbe05af93bc277435a4a7be1330579c78bb2ba573b4e555bf12ef9c9`；两清单SHA256 `3ff47ebd47869d8105d4f3672ddc3117b39c57b67052b7dcb7890d4f0264169a`，214资产，JSON/Markdown报告同目录。构建基于76290c32的dirty源码，非最终提交发布构建；旧嵌入备份 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-1810-8730uv/dist` 保留，不覆盖运行程序。
- 仍需核清旧日志身份迁移、非Binance资产元数据、缺历史定义恢复、其他配置/热更新门禁、运行时更新失败明确反馈、跨进程原子保护；已持久化后updater接口仅返回IDs，不能证明每个运行时已成功应用。未运行严格MySQL/远端CI/浏览器E2E，不改main/tag、推送、部署、真实账户或生产数据；推送仍待目的地批准。R01–R15完整目标保持，实盘和盈利未验收。

## rc973后续复查：策略写入口仍可改掉待核账恢复身份（红测已复现，未修复）

- 独立SQLite overlay两项安全断言失败：同类型SpotShort策略修改group_id/symbol均HTTP200，主配置实际改变且原schema9待借款payload保留；修改前rc973门禁为Required，修改后Unverified。钱包预留确认不存在，不能依赖预留保护这个路径。
- [专项证据和修复验收要求](2026-10-03-strategy-write-recovery-review.md)。这是四入口之外的真实缺口，不扩大rc973已验证范围；下一步需生命周期内最新配置/完整金融状态核验及原子写入策略，同时保留运行中正常风险调整，避免粗暴封锁带来的能力退化。
- 本轮只有审查文档和临时隔离测试，未改生产源码/版本/main/tag、推送、部署或真实账户。两项失败不作验收通过，完整目标保持。

## rc973：四入口生命周期内金融日志门禁（本地验证完成）

- 配置文件PUT/DELETE、Bot DELETE、组DELETE在既有生命周期锁/有序停止协调回调内，从同一主存储完整读取全部Bot策略记录，以独立配置身份核验每条。有效未完成经济游标409；读取失败/取消、nil、未知策略/schema、缺历史定义、错账户及重复Bot身份503。合法平账与确认无记录正常放行。组内所有成员先核验，再开始配置删除；不释放钱包预留或改写金融payload。
- 普通策略复用生产归一化与重复检查；对冲按实际直接读取Bot实例路径绑定group/symbol，基础币仍依据适配器所用Bot符号。Combo显式Bot symbol覆盖raw参数；自定义旧子定义缺失保持未核实，不从金融payload猜测。账户摘要共享原运行时协议。离线基础币仅支持明确Binance稳定币符号契约，不是实时交易所元数据/库存证明。
- 隔离SQLite永久回归使用真实HTTP处理器和生命周期fixture provider，匹配账户身份的pending/历史剩余资产先单独证明ErrRecoveryConfigRequired，四入口409并读回原主配置、文档及payload；错误/旧证据503，合法平账/无记录200。覆盖组第二成员待核账不能先删第一成员、锁内晚到记录、取消/读失败及重复Bot身份。不冒充默认BotManager全流程或浏览器E2E。
- 首轮失败是通用测试夹具附带非马丁格尔无关Direction，以及legacy环境Bot下标/总数假设；修正为契约和明确ID、前后读回后通过。新增对冲测试首次误用不存在snapshot方法编译失败，改用真实持久化序列化后复验；未隐藏失败。
- 最终策略完整race139.869s、Web完整race40.906s、最终生产源码根包22.532s/配置1.849s通过；绑定/四入口三轮race策略2.149s、Web28.695s通过，随后新增重复身份夹具包含在最终Web完整结果。vet/diff通过；Yarn verify整链通过，独立dot读回45文件225项；Ruby嵌入14runs27assertions/交易9runs58assertions均无失败、错误、跳过。原八项overlay策略1.882s/Web4.389s通过，但其拒绝来自scope不匹配503，不代替匹配身份409证明。
- Go+React嵌入技能用于本地构建：Make通过后在稳定清单上重建最终后端，compiled embed1.105s通过，临时二进制--version为3.111.0-rc973，API版本接线保持。报告 `/private/tmp/quantmesh-config-gate-rc973.Jj4sc0/results.{json,md}`；二进制SHA256 `3e33d5ff6689bd535b5a6c35c2d811186fa78268a66a7bdf03d86bd3b53e5a88`，两前端清单SHA256 `cf048a788232575413d07f517af2ec9ea51f839a0ba18970f58153c157fb65ed`，214资产。构建基于b6dff013的dirty源码，非最终提交发布构建。旧嵌入备份 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-98245-kj2ere/dist` 保留，无运行程序覆盖。
- 尚缺其他配置/策略参数写入及热更新门禁、非Binance独立资产元数据、移除自定义定义的恢复能力、双文档原子持久化及跨进程fencing。下一步沿实际写入链复现绕过并补恢复能力，不能以长期封锁代替闭环；R01–R15目标保持。未运行本版严格MySQL/远端CI/浏览器E2E，不继承旧证据；不改main/tag、推送、部署、真实账户或生产数据，目的地批准仍未收到，不宣称实盘或盈利验收。

## rc972：对冲日志与完整Bot记录集合核验（生产入口尚未接线）

- 补齐SpotLong、SpotShort、FuturesLong/Short和Combo父记录只读核验。现货待办订单/借款/买回/还款、期货预提交订单不能被日志缺失之外的零值掩盖；已确认正ID的消费还款历史允许保留，不以有历史记录永久阻止正常平账。未知schema、缺字段/null、错group/资产/身份、非法消费还款ID不返回已核清；非空待办只要求保留配置，不依赖此处把其恢复证据验成可交易。
- 新增完整Bot状态集合核验，显式类型/独立绑定分派全部13种受支持类型/别名，所有记录均需核验；不按当前enabled配置过滤，也不根据JSON形状猜旧策略类型。nil集合是不可用，non-nil空集合是调用方已确认无记录；拒绝nil/重复/跨Bot记录及缺失/重复绑定。类型和绑定来自独立配置/身份的契约仍须实际生产解析器接线，函数自身不能证明调用方绑定来源可信或传入没有截断的集合；必须使用rc969完整Bot读取，不能把任意部分列表作证。
- Combo子键复用实际producer的父名SHA256前16字节命名规则，提取为共同纯函数且保持原读取/写入校验与128字节限制。子记录要求父类型/身份/symbol及实际父记录存在，不允许嵌套父关系或不支持的子类型；无论输入顺序父先/子先，父状态正常不能遮蔽子待办。无记录只证明该完整快照缺少耐久日志，父记录单独并非子库存/实时账户证明。
- 全部RecoveryConfig定向三轮race2.332s通过：实际SpotLong/Short及期货tracker保存流程，SpotShort历史还款/已借未完成、期货预提交，13类分派正常及schema拒绝；真实临时SQLite105条旧signal记录末端待办/漏绑定拒绝，全部核清允许，数据库直接读回未改原payload；Combo六类子策略正常、未完成、顺序、缺父/错父保护；nil/重复/跨Bot/未知类型和已取消context拒绝。首次fixture给lazy nil消费映射直接赋值导致panic，初始化测试映射后重跑，不改生产构造行为。中途context取消未独立注入，不将预取消测试扩大为该证据。
- 最终源码完整strategy race139.750s完成通过，JSON/Markdown终态报告位于上述临时目录results.{json,md}；不将运行中测试视为通过，不继承旧版回归记录。
- Yarn verify及独立dot读回45文件225项通过；Go+React嵌入技能用于前端先构建、Make临时二进制及compiled embed1.066s通过，API版本header原接线保持，前后端rc972。vet/diff和Ruby嵌入14runs27assertions/交易9runs58assertions通过。目录 `/private/tmp/quantmesh-state-collection-rc972.oJAYgg/`；二进制SHA256 `d9760251784e29aecdf53b412db66fc6dbb5cad4e3da2a63f0f80ff3b942d99c`，两清单SHA256 `ca8d4831e1b723fa508028baf8134c768127db93cd2aaaa60574aa4d8eda5973`，214资产；旧嵌入备份 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-94722-dsckbo/dist` 保留，未覆盖运行程序。
- 当前SQLite套利overlay兼容race1.809s通过；四入口八项安全断言仍失败（Web3.063s，退出1）。新增集合函数尚无生产绑定解析器、生命周期内完整读取或四入口调用；不能称真实保护已经生效。下一步必须补独立绑定解析（含旧/禁用记录及缺失绑定保持恢复能力）、默认管理器/四入口同一核验接线、正常核清/失败放行边界与数据库读回、永久回归及E2E；其他配置写路径、旧schema完整核账与跨进程fencing也未闭合。
- 此处订单/借贷日志核清不证明实时现货、保证金资产/负债、当前所有权、资本释放或盈利；不继承旧严格MySQL/远端CI/目标平台。构建基于95176be1上的dirty源码，非发布提交。R01–R15完整目标保持，未改main/tag、推送、部署或访问生产数据/真实账户；推送目的地人工批准仍未收到。

## rc971：DCA/马丁格尔/信号耐久平账的配置保留判断（尚未接入四入口）

- 新增三类只读纯函数，要求独立Bot/策略名/symbol（马丁格尔另要求LONG/SHORT），使用真实私有schema及既有资本释放空仓判断检查条目、平仓意图、成交/费用游标和精确非零数量；不能用空仓标志或策略仍在配置中作证。允许自定义/Combo底层子名称，但记录键与Combo命名空间、类型映射的集合核验尚未实现。
- 实际DCA/马丁格尔serializer将空层/条目保存为null，不再用通用null禁止规则误判合法平账；只在对应root集合允许，嵌套null条目、进度/统计null均拒绝。统计与close_progress要求真实Go序列化的完整字段（Quantity/Notional、TotalTrades/WinRate/TotalPnL/TotalVolume）；缺失、重复键/大小写覆盖、尾随或未知数据不能被零值补成平账。此前套利null拒绝规则保持。
- 该核验证明耐久经济游标没有未处理内容，不证明当前账户库存或资本释放；非空条目/持仓/订单等直接保留配置，不靠这一步完成其有效性核账或下单恢复。无效/不足证据与需保留恢复配置以既有sentinel区分。正常暂停、历史亏损/利润、合法空集合不永久阻止平账；旧schema不能冒充当前证据，后续仍须明确兼容核账能力。
- 定向全部RecoveryConfig测试三轮race1.629s通过：真实三个持久化producer保存并读回合法平账，包含暂停、历史亏损及自定义子名；UNKNOWN条目、未完成订单/平仓/费用游标、负平仓损益、最小正数保持拒绝；错Bot/策略/symbol、未知schema、嵌套缺字段/null、nil条目、重复统计键等不足证据拒绝。纯函数仅收字符串和绑定，不接真实交易、存储写入或金融RPC，原payload不变。
- 最终源码策略完整race139.604s完成通过；前端verify与独立dot读回均45文件225项通过。不将运行中测试算作终态，不继承旧版本通过记录。
- 前端Yarn verify完成、Vite/PWA构建通过；Go+React嵌入技能用于先前端后临时二进制，Make和compiled embed1.130s通过，API原版本header接线保持，前后端rc971一致。vet/diff通过，Ruby嵌入14runs27assertions/交易9runs58assertions通过。临时产物与JSON/Markdown报告目录 `/private/tmp/quantmesh-single-leg-proof-rc971.Nt5BBJ/`，二进制SHA256 `8bee3b898211c0a9492acc1b18d2f6336a08aa1c5c3c29bdefbd30397a842f81`，两清单SHA256 `b84b0c710ad197c05db946a968284f4b5170e584fff473cd807b4e234a9e14db`，214资产；旧嵌入可恢复备份 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-92932-kmtypw/dist` 保留，不覆盖运行程序。
- 原临时SQLite套利overlay兼容race1.771s通过；四入口八项安全断言仍失败（Web3.449s、退出1），金融payload不变但配置依然可被改掉。现货/期货对冲、Combo父子记录映射、所有Bot记录的独立绑定、旧schema核账与四入口统一接线、其他写路径、正常核清放行、默认adapter/E2E与跨进程fencing均未闭合，不宣称本轮修复开放入口缺陷。
- 不继承旧严格MySQL/远端CI/目标发布证据；本地构建为c8d5d166上的dirty源码，非最终发布提交。R01–R15完整目标保持活跃，未改main/tag、推送、部署或访问生产库/真实账户；推送目的地人工批准仍未收到。

## rc970：套利耐久状态的只读配置保留核验（尚未接入入口）

- 新增 Funding Carry / Funding Perp Spread 纯函数，调用既有实际恢复解码器及借贷、成交费用/确认消耗校验；要求独立非空身份绑定，Carry 明确账户 scope 和基础币。已核清耐久记录返回 nil，需要核账与无效证据通过不同 sentinel 错误及 errors.Is 区分，不输出完整金融 payload。
- Carry 在正常本金/仓位清零后仍精确核算历史余量；0.0008 及 nextafter 微量正余量不得消失。双永续额外核查 execution ledger、emergency close、pending order/executions 和两腿有符号数量，不能仅靠零仓位证明核清。只是耐久账本判断，不证明实时资产、可支用余额、账户所有权或释放资金安全。
- 对当前 schema6 的完整序列化字段核验：缺失基础字段、null、任何层级重复键（含大小写覆盖）、未知字段、尾随数据拒绝；采用深度上限。旧schema、未知schema及缺少独立绑定不返回已核清。旧schema后续须明确兼容核账路径，不能让拒绝永久替代恢复能力；当前函数未投入生产配置准入，不引入旧Bot永久禁止修改的入口行为。
- 最终定向三轮 race 1.812s 通过，包含合法零差额、两类余量、待办标志、错账户/资产、旧schema、缺字段/null/重复键、未知字段，双永续未核实账本、紧急平仓、合法 pending order/execution、负仓位和最小正数。首轮旧夹具交易所名为空导致拒绝，补齐夹具名称后重跑，不放宽生产校验。输入为字符串和绑定，不接交易所、存储写入或金融RPC。
- 策略包完整race139.884s完成通过，使用最终生产实现；全量运行期间仅追加测试覆盖，新增断言已由随后定向三轮验证，不将运行中测试算通过。JSON/Markdown报告在 `/private/tmp/quantmesh-recovery-proof-rc970.yGiuH0/results.{json,md}`。
- Yarn verify 45文件225项及Vite/PWA完成；Go+React嵌入技能用于前端先构建、清单sync/verify及临时二进制构建，编译内嵌测试1.082s通过。首次Go缓存权限阻止构建/vet，原命令允许环境重跑通过；Ruby嵌入14runs/27assertions与交易9runs/58assertions通过，最终vet/diff检查通过。二进制SHA256 `414b91d7677d27e4abdc0827ab445f314677e402e6f43319ce84e65a689febd3`，两清单SHA256 `a5106bf0f1cfe2e9a8c1a700b58fa09c427ab2ef8569de2b44503aff66f75034`，214资产；旧嵌入保留于 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-90994-1npws/dist`。
- 真实SQLite原overlay兼容性1.750s通过，四入口两种状态仍8项安全断言失败（Web3.068s，退出1），金融payload保持但配置依然可变。此轮不得称四入口缺陷已修复；尚须所有策略schema及旧记录身份核验、读取接口/管理器/四入口统一接线、正常核清放行、默认适配器及浏览器E2E、跨进程fencing。R01–R15完整范围保持。
- 没有本版严格MySQL、远端CI、同提交发布或实盘盈利证据；构建验证基于dd9cbc5b上的dirty源码，非发布提交。未改main/tag、推送、部署、真实账户或生产数据；推送目的地批准仍未收到。

## rc969：统一金融状态保护前的完整Bot记录读取（存储步骤本地验证完成）

- 新增可选BotStrategyRuntimeStateContextLister及SQL实现：按参数化Bot身份读取全部策略记录，不依赖现配置策略名，不跨Bot，不设静默分页截断；保留strategy/schema/payload/updated_at并按strategy稳定排序。已知无记录返回非nil空slice；取消、连接等待、查询/扫描/迭代/关闭失败返回错误和nil，不能拿先前部分记录证明全部核清。使用既有表/主键，不新增DDL或改旧读取接口。
- 最终定向三轮race2.368s通过：105记录完整性、旧策略/跨Bot隔离、参数化SQL、原字段保持、等待单连接取消及正常恢复，真实SQLite第二行损坏timestamp证明首行扫描成功后的失败不会暴露部分记录。夹具清理错误不吞掉；最后校准后重新跑定向和全量。关闭驱动错误/中途迭代取消尚未独立注入，不能以逻辑已接入冒充逐分支证据。
- 最终根包/存储完整race24.316s/12.658s，857 pass测试事件、8 MySQL skip、零fail；事件数包含父测试，不作为独立叶子数量。两包vet/diff检查通过。当前源码另重跑既有overlay：两类有效金融夹具通过、八项API安全断言仍失败（go退出1），直接读回确认新接口尚未保护配置入口，不掩盖开放缺陷。
- Yarn verify完整链45文件225项及Vite PWA、Ruby嵌入14runs/27assertions与交易9runs/58assertions完成通过。按Go+React嵌入流程Make、清单verify、编译内嵌字节1.561s通过；临时二进制版本3.111.0-rc969。最终夹具清理校准只改测试，不改生产二进制源码。报告 `/private/tmp/quantmesh-bot-state-reader-rc969.Pd2dk8/results.{json,md}`；二进制SHA256 `9a8d7029922d7877123fe6e681b23523b62044d5145ed082bd4d8a09d01264c7`，两份前端清单SHA256 `133d71862496697762597e22eafa2205129b4316b9a2d79d4c5a7058b46759d9`，214资产。验证于HEADa2ca4b08的dirty源码，非最终发布提交构建；旧嵌入备份 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-88712-15n2fq/dist` 保留，不覆盖运行程序。
- 这是原统一保护方案的必要存储步骤；未以只读接口替代金融语义核验和四入口接线，八项开放红测仍未修复。各策略正常核清/UNKNOWN/借贷/历史资产schema核验、缺失或未知数据拒绝、配置恢复状态保持、跨实例fencing及R01–R15完整范围均继续保留，版本前后端rc969一致。
- 没有本版严格MySQL、远端CI、目标平台或默认适配器/浏览器E2E证明，不继承旧数据库验收；未改main/tag/推送/发布/部署，目的地推送确认仍未收到，目标保持活跃而非完成/阻塞。

## rc968后续审查：无预留但有金融状态时仍可丢失恢复配置（已复现，未修复）

- 基线精确为7218cb432a03fd6dd85e80327a6ba6cdea0a88eb，无业务源码改动。独立overlay和临时SQLite确认两类有效schema6金融状态：未完成操作与标志已清零但历史剩余0.0008BTC；真实恢复解码器接受恢复模式，严格模式均拒绝，夹具兼容race1.764s通过。初轮成交字段用错JSON格式导致无效，校准后重跑全部证据，未忽略该失败。
- 八项API红测真实读回：确认无预留的两类状态均可通过PUT config-file、DELETE config-file、DELETE Bot、DELETE group返回200，金融payload不变但配置文档变化；PUT/Bot删除/组删除同时改变或移除主身份，配置文档删除单项仍有主配置fallback。八项安全断言失败是开放缺陷而非回归通过，也不是线上已经丢失数据的证明。
- 详细范围、下一步schema核验/全Bot耐久记录读取/四入口一致保护/正常核清放行要求见[专项复查](2026-10-03-orphan-runtime-state-config-review.md)，JSON/Markdown与可重跑overlay在 `/private/tmp/quantmesh-orphan-state-audit.SuqaY2/`。生命周期提供者为隔离夹具，不冒充默认适配器HTTP/浏览器E2E。仅追加审查文档，不递增业务版本；R01–R15目标保持，不标完成/阻塞，未改main/tag/推送/生产账户。

## rc968：配置文件入口不得抹掉持有预留的失败实例恢复配置（本地验证完成）

- 真实临时SQLite红测确认：停止/未受管实例有50USDT钱包预留，PUT整份配置和DELETE配置文档仍200；旧代码明确进入覆盖BTCUSDT为ETHUSDT/删除分支。红测在状态断言处失败，不把成功日志冒充红测数据库变化读回。现有Bot删除保护并未覆盖这两个配置文件入口，不以“启动失败”推定无资产。
- 新增明确生命周期协调接口，实际BotManager和Web适配器接线；同一Bot启动/停止和配置核查/写入共用生命周期锁，进程退出拒绝新修改。存在受管实例（即使非交易循环）直接拒绝，不自动Stop；Web在锁内重新核查实例及持久化预留，核查失败或缺少协调接口503，有预留409，确认无预留时保留原更新/删除行为。
- 最终定向三轮race根包3.092s/Web12.554s，根包和Web完整race28.588s/61.723s、两包vet/diff检查终态通过。修复后真实SQLite读回确认原配置文档Content、主Bot身份及50USDT预留不变；新预留/实例在协调准入期间出现也拒绝，存储查询失败和缺少协调器503，已停止且确认无预留的正常操作200并读回。真实BotManager启动验证被配置持久化锁排除，拒绝受管实例且零Stop调用；退出拒绝及失败回调解锁通过。Web夹具协调器与真实管理器分别验证，不冒充默认适配器完整HTTP/浏览器E2E。
- Yarn verify完整链45文件225测试、Ruby嵌入门禁14runs/27assertions、交易门禁9runs/58assertions通过；按Go+React嵌入流程Make构建、清单verify、编译内嵌字节1.579s完成。报告 `/private/tmp/quantmesh-config-recovery-rc968.PBulMJ/results.{json,md}`；二进制版本3.111.0-rc968，SHA256 `80916e1fc12f1a017a606daec56570bcbfbbff6330c49b5e7161fb5661972b77`，两份前端清单SHA256 `b09c36b6a7605c53dfd071dcffa8107716dcc15f4487cb3035630738bafd61f2`，214资产。验证于HEAD92216f5e上dirty源码，不是最终发布提交构建。旧嵌入备份 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-87305-jogadv/dist` 保留，无运行程序覆盖。
- 改动限于PUT/DELETE config-file及协调接线；其他策略参数入口、全局配置/热更新、跨实例数据库原子fencing、没有预留但有金融状态的独立保护及配置双文档原子持久化仍需审查，不宣称全配置写路径或R09完成。不继承严格MySQL/远端CI/目标平台证据；未删生产数据/真实账户，未改main/tag/推送，推送目的地确认仍未收到。

## rc967：前置失败不能隐藏历史钱包预留（本地验证完成）

- 真实临时SQLite完整构造器先建立受管核账实例，再停止并读回三钱包各50USDT和原schema6金融payload；重试预检失败/已取消两个红测均复现只有普通原错误，历史预留未出现在返回诊断。
- 有持久化服务且有效构造器输入时，在全部失败清理完成后只读核查同一Bot历史预留。取消路径使用独立五秒context；持有预留、读取失败或接口缺失加入结构化未核实诊断，原失败/读取原因仍可errors.Is追溯。确认无预留原样返回，已有保留诊断不重复查询；成功路径不查询。nil存储及无效构造参数不承诺历史存储核验。
- 不建额外交易连接、不释放/改写预留或金融payload，不将前置失败准入为受管核账运行时；该查询是当时数据库快照，不证明资产已平仓或跨实例原子fencing。完整失败实例管理接管仍未完成。
- 定向三轮race3.416s、根包完整race25.424s、根包vet/diff检查终态通过，覆盖真实构造器重试、取消独立读取、读失败/接口缺失及准入拒绝。Yarn verify整条链及独立dot报告45文件225项通过；Ruby嵌入门禁14runs/27assertions、交易门禁9runs/58assertions全部通过。按Go+React嵌入流程完成Make构建、磁盘清单验证及编译内嵌字节测试1.710s，临时二进制版本3.111.0-rc967。
- JSON/Markdown报告位于 `/private/tmp/quantmesh-startup-audit-rc967.zt24tZ/results.{json,md}`；二进制SHA256 `f3c21b6738cacf33765630bb8eed0dafb1c9e389745f39cd152aa44c8c89189c`，两份前端清单SHA256 `a6d55009abe665417e118f88e4ea051bd51e59ca21ecc2c1fe8b81c5502a1295`、214资产。验证时HEAD4721d247且源码dirty，非最终提交/发布构建；旧嵌入备份 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-86270-rce16e/dist` 保留。元数据读回脚本首次用错键失败，改用实际files字段后读回/verify完成，不掩盖失败。
- 未运行本版严格MySQL、远端CI或浏览器E2E，不继承rc966/旧严格证据；R01–R15范围保持，不改main/tag/部署/真实账户，推送目的地确认仍未收到。

## rc966：完整构造器的剩余资产接管与清理保留诊断（本地验证完成）

- 不再只测试准入辅助函数：实际生产构造器委托实现接入隔离预检/交易所和t.TempDir SQLite，真实配置、钱包观测序号、三钱包预留、执行意图后端、策略耐久适配器及PriceMonitor均运行。成功的历史剩余资产路径可返回SymbolRuntime并导入0.0008 BTC历史量；不启交易循环，StopWithError拒绝平仓/资金释放，三钱包各50USDT及schema6原始金融payload均保持。
- 将实际构造结果加入真实BotManager的AddRuntime注册表并读出Bot状态映射，确认“受管但不交易且待核账”；这是注册接线，不冒充默认StartBot自动构造入口或浏览器验收。夹具明确拒绝并计数Place/Cancel/Borrow/Repay/Transfer，核验零金融RPC及价格/订单流清理。
- 错账户完整构造器红测读回：三项预留和原始金融payload确实被保留、没有金融变更，但返回错误仅含原启动原因，清理拒绝释放仅在日志。新增fundingCarryStartupRetentionError并与原错误Join，覆盖资金核清失败、策略停止失败及所有权丢失/冻结失败；不准入为正常核账实例，不自动释放资金或接管错账户。
- 诊断措辞为“预留释放未核实”，不把存储提交报错当作已读回预留必然存在；错账户/停止失败夹具另以数据库实际读回证明三项预留仍在。单原因包装的保留诊断同样拒绝准入，不仅依赖外层Join。其他所有权丢失/策略停止失败分支接入同类型返回，但尚缺逐分支完整构造器故障注入证明。
- 最终定向三轮race根包4.031s、根包完整race29.528s终态通过，根包vet、diff检查通过；包含实际临时SQLite构造器与原取消/所有权/受管准入回归。错误账户的原启动原因及结构化释放未核实原因均返回，原payload不变，三个钱包预留真实读回均为50USDT；单原因诊断包装也不能恢复准入。没有以日志代替数据库读回。
- Yarn verify整条链条成功，类型检查/测试/Vite PWA构建通过；独立dot报告读回45文件/225项测试全部通过。Ruby嵌入门禁14runs/27assertions、原交易门禁9runs/58assertions均零失败/错误/跳过。完整Make构建成功；最终诊断措辞校准后，在前端清单不变且核验通过的条件下重新go build最终源码，--version返回3.111.0-rc966，编译内嵌字节测试2.297s通过。
- 最终二进制 `/private/tmp/quantmesh-constructor-recovery-rc966.gOnIBM/quantmesh` SHA256 `3cd3f899add06887a3414d0eaa8ef98dcbd5767c6b2e94608e55743bb1c6d9a9`；两份前端清单均为 `6f17c818a8116777602c84971e67f5b6089e241770485eb81178c9175a64ebf3`（214资产），来源摘要 `3c41d8444198b1c13fc30e22563a4b8a1a8e691d7846c8d1c1be9acdbe7c282f`。构建于HEAD45856458的dirty源码，非发布提交构建。JSON/Markdown在上述专用目录；上一轮嵌入目录保留于 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-85253-3gmc6a/dist`，无现有运行程序覆盖。
- 完整前置失败管理接管、当前库存/资产处置、UNKNOWN/部分成交补偿、原子fencing和R01–R15其余要求仍未完成；本轮SQLite不冒充严格MySQL/跨进程行锁证明，不继承旧数据库/远端CI或盈利证据，不改main/tag/发布/部署，推送仍待目的地确认。

## rc965：套利运行时不能写入其他 Bot 的策略 map（本地验证完成）

- 继续核对 R09 生产初始化/恢复前提时发现两个专用构造器均浅拷贝 baseCfg，再直接写共享 Strategies.Configs。串行红测确认 Funding Carry/Perp Spread 都改变基础配置；Funding Carry 真实构造器入口在注入工厂失败后，仍覆写共享策略权重/启用/参数。未用标题或race未复现臆测问题。
- 两处合并先 maps.Clone 外层 map，再写当前 Bot 的专用条目，保留无关策略及原 Bot 参数选择/默认权重。该路径仅读取条目内Config，没有宣称所有嵌套值深拷贝或同时修改基础配置的热更新已安全；全构造器成功路径、Perp Spread全入口及失败实例接管仍待验证。
- 双入口合并、失败Funding Carry构造器、24个并发本地合并及原取消/受管准入回归连续三轮race终态通过（根包2.349s）；根包完整race23.962s、根包vet和diff检查通过。并发夹具仅并发合并不可变基础配置，不用于证明热更新同时写入基础map的安全性；Perp Spread只覆盖同生产合并函数，不冒充其全构造器成功/恢复。
- Yarn verify整条命令成功，类型检查、前端测试及Vite/PWA构建通过；另以dot报告读回45文件/225项测试全部通过。嵌入门禁14runs/27assertions及原交易门禁9runs/58assertions均零失败/错误/跳过。
- `make build OUTPUT=/private/tmp/quantmesh-funding-config-rc965.6l3uOd/quantmesh` 终态成功，--version返回3.111.0-rc965；最终稳定后编译内嵌字节测试1.551s通过，未与前端重构建重叠。产物SHA256 `fc6d0094e58d6bf778df479ea5eae69ca4c1788c102962ccc4d46ae2f96527c9`，两份前端清单均为 `e1c9125308b9aa8e6b47679cea835aead29a2eb6295e068c47fc52374b2132b1`（214资产），来源摘要 `cffa8b9121f63f5deb3c32ea98f673910aae93fc403f443ebe80204b2cfbb30b`。构建时HEAD45328980且源码dirty，非发布提交构建；JSON/Markdown结果在上述目录。
- 上轮嵌入保留于 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-83938-tqepb0/dist`，不覆盖现有运行程序。未继承旧严格MySQL或远端CI结果。不解除资金预留或交易封锁；R01–R15范围保持，完整资产恢复/处置、目标平台交付与净盈利仍未验收。推送目的地仍待确认，不重试被拒绝操作，不改main/tag/发布/部署或连接真实账户。

## rc964：专用构造器取消边界（本地验证完成）

- 原生产构造器预检返回后未检查调用方取消，实际前置/预检中取消均继续进入所有权初始化；初始价格使用不可取消的Sleep，已取消但有缓存报价时仍返回成功。先保持原语义并注入显式预检/工厂依赖复现四项失败；不替换全局变量、不访问真实交易所。
- 同一生产入口委托显式依赖实现，构造前及预检/工厂/余额读取返回后检查取消；保留取消与RPC/清理错误，失败清理登记提前到首个连接创建，含价格启动失败。报价等待使用可取消timer，保留原10次等待预算，拒绝NaN/Inf；没有放宽资金、所有权、金融意图或账户核验。
- 最终定向三轮race通过（根包2.498s），覆盖6组新增测试与原受管准入/所有权回归：预检前/预检中取消、首个连接构造期间取消及流清理失败、进入报价等待后取消、缓存报价不能覆盖取消、非法报价和两项租约清理失败均可追溯。根包完整race终态25.517s通过，根包vet、diff检查通过；无真实账户/下单。余额及后续连接分支有生产取消检查，但尚无完整构造器贯穿全部分支的端到端夹具，不据此宣称全部初始化成功/失败路径验收。
- `yarn --cwd webui verify` 整条命令终态成功：类型检查、45文件/225测试、Vite/PWA构建通过；Ruby嵌入门禁14runs/27assertions及交易门禁9runs/58assertions，均零失败/错误/跳过。完整 `make build OUTPUT=/private/tmp/quantmesh-funding-startup-rc964.BvaYDj/quantmesh` 成功，`--version` 返回3.111.0-rc964；最终稳定后的编译内嵌字节测试1.419s通过，不与前端构建重叠。
- 产物SHA256 `f1845609accf729c002eb63f3442b4d983a4fce044860f8c67bd863483de7408`，两个前端清单均为 `af76b00beaeca3ce26d1286b7d5254c7e0531620ca9c747cd0267fc8b518bbc3`（214资产）；来源摘要 `65ad97be79a97984c6d24cb3932b944db7056c35f348ff1c1da1982225a04b7d`。构建于dirty源码及HEAD882422ad，非发布提交产物；JSON/Markdown结果在上述专用目录。上轮嵌入产物保留于 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-83177-p9v09s/dist`，不覆盖现有运行程序。
- 本项不冒充完整失败受管实例接管：预检拒绝、资产历史状态注册、当前库存、处置与恢复交易仍未闭合，工厂没有context参数仍不能中止已进入的构造调用。未继承rc962严格MySQL、未核验远端CI/目标平台发布。R01–R15范围保持，推送待用户确认目的地，不重试或切凭据，未改main/tag/发布/部署或连接真实账户。

## rc963：前端嵌入交付来源与失败门禁（本地验证完成）

- 对应 R14：标准 Makefile、scripts/build.sh 与 CI/CD 使用 Yarn 前端构建及 Ruby 门禁，删除构建中的忽略复制失败、缺前端跳过和占位回退；Make 依赖保证并行构建仍先前端后 Go，不从历史 git tag 覆盖源码版本。
- build-meta.json 绑定前后端版本、前端 src/public/配置及门禁脚本摘要、完整产物 SHA256；失败构建先撤销旧标记，源码在构建过程中改变不得发新标记。同步先核验临时副本，保留旧嵌入目录；复制或交换失败保留/恢复原目录，拒绝产物符号链接和越界引用。来源目录仅允许指向同目录内普通文件的链接，并同时核验链接身份与目标内容，兼容仓库已有 PWA 图标链接；越界/目录链接不接受。
- 新增 opt-in embedded_frontend Go 测试，直接读取编译进测试程序的 go:embed 元数据及全部资产字节，与当前核验前端清单比对；CI/CD 标准测试明确执行，不仅检查磁盘复制。
- 最终门禁14 runs/27 assertions，原交易门禁9 runs/58 assertions，均零失败/错误/跳过；覆盖 `make -j4` 前端失败不得进入 Go、内部来源链接及目标变更、越界链接、失败交换恢复。首次构建因现有8个PWA图标链接被过度拒绝而失败，补红测并修正上述来源边界后才接受结果。前端类型检查及225测试通过，后续独立构建和完整 Make 构建通过；没有将最初失败的 yarn verify 整体链条写成成功。
- `make build OUTPUT=/private/tmp/quantmesh-embed-rc963-build.FWYjEh/quantmesh` 成功，`--version` 返回3.111.0-rc963。产物SHA256 `a911e9a0e097484f3ed5048d353fe4b41aa22b2d9bc35a857a30f2341bf94c00`；webui/dist与web/dist清单均为 `a33853ef7675468634187e3b5c3156c6fa5c3ff953d00893e97e67dfd8a07cc8`，包含214个资产，来源摘要 `ece7e39ed9126ce9a31a6ad99fa8ed5ae944ea6d547dbde2cf5d56db88533253`。本地dirty源码构建，Vite内Git短号仍指向构建时父提交，不冒充最终发布提交构建。
- 编译内嵌测试在最终产物稳定后成功（web1.940s），根包/Web全包race终态成功（38.505s/82.696s），根包/Web vet、YAML解析、bash语法及diff检查通过。初次Go检查受缓存权限阻断；获准重跑时与前端重构建重叠导致标记缺失，该失败不作为验收，待构建完成后重新执行成功。此处无严格MySQL fixture/零跳过数据库证明，不继承rc962报告。JSON及Markdown本地记录位于上述专用临时目录。
- 首次同步将原旧嵌入产物保留于 `/var/folders/np/rjc0y5w52x324x21pv6g33440000gp/T/quantmesh-embedded-backup-20261003-81753-pbboil/dist`；Make再次同步保留上轮产物于 `quantmesh-embedded-backup-20261003-81911-9moh1k/dist`（同临时根目录）。未删除旧产物或覆盖现有运行程序。
- 不把来源摘要当作外部环境/依赖安装可重现证明；历史 scripts/build-release.sh、远端CI/实际目标平台发行构建、真实账户资产处置、完整恢复、原子 fencing 和净盈利仍未验收。R01–R15范围保持，未改 main、打 tag、发布、部署或连接真实账户。

## rc962：同提交严格数据库验证与嵌入前端证据边界

- 被测代码提交精确为 `186818f634fe9486b927c04a58f0499db5898045`，版本 `3.111.0-rc962`。测试前后HEAD相同，tracked diff和index diff均为空；报告source_dirty=true仅因用户无关的`?? --help/`，该目录未打开/修改/暂存。后续本次提交仅补文档，不冒充文档子提交另跑过全部测试。
- 新建MySQL8.0.36一次性fixture，镜像`sha256:a532724022429812ec797c285c1b540a644c15e248579c6bfdf12a8fbaab4964`，schema `quantmesh_readiness_rc962` 初始表数0。仅绑定127.0.0.1:32780，无宿主目录挂载，512MiB tmpfs、1GiB内存/2CPU；临时破坏性迁移开关仅指向该空库，不读取生产配置/账户/凭据。
- 执行`ruby scripts/verify_trading_race.rb /private/tmp/quantmesh-trading-race-rc962-same-commit-mysql --require-mysql`，临时DSN仅指向上述fixture。`results.json`及`results.md`均读回1941 pass、零skip/失败，mysql_required=true、无缺包/缺数据库证据/解析错误；strategy141.145s、storage18.723s、Web57.700s。
- 8项强制数据库用例均为pass：独立暂停owner、资金费身份/覆盖、共享钱包资金预留、借币利息账本/覆盖、逐笔成交覆盖迁移、现货快照迁移、模糊Bot归属回填拒绝、收益提现规则。额外nilDB配置测试不用于冒充这8项证据。
- 结束后再次核验容器身份/专用标签/无宿主挂载，只删除精确容器`3e8fd7ed71b3f05c823dd35439b48d320cf731e96dfd35a56e936e5ed2d2f5e5`；按ID和名称查询均无残留。tmpfs测试数据已丢弃，报告与缓存镜像保留，未删除其他容器、仓库文件或生产数据。
- 按Go+React嵌入流程另查R14交付边界：`web/static.go`嵌入`web/dist/*`，本地该目录与最新`webui/dist`不是同一目录；index SHA256分别为`887c3d0eb5c31690504bbff8c36103245955963f092d8c5500fc1f692a5d242d`和`2bea7973b5062c41adb2fa8f3710f7cd3712e7fd58e7d70f1d9bc15e8ab35f15`，当前rc962版本字符串仅在最新webui产物找到。CI标准测试步骤明确复制新产物，但CI/CD其他构建分支存在`cp ... || true`及占位回退，尚需统一失败门禁及同步证明；不将本地差异直接宣称线上已部署旧前端。
- 此严格报告补齐当前提交的数据库回归，不证明最新前端嵌入产物、目标平台发行构建、当前真实资产归属/处置、完整故障恢复、原子fencing或净盈利。未同步/改写嵌入产物以干扰本轮相同输入，未改main、打tag、发布、部署或连接真实账户；R01–R15未完成事项继续保留。

## rc962：启动回滚冻结顺序与多重失败不能丢失

- 两个定向红测分别复现：StartAll只返回原启动错误、丢失失败项及先前项的停止失败；回滚开始时先前成功项的循环context仍活跃，可能在其他清理阻塞期间继续决策。不是把日志中的失败当作代码已返回的证据。
- 启动失败先取消共享子context，再调用失败项和先前成功项Stop；仅发送取消，不能冒充全部在途RPC已终止或资产已平仓。管理器收集所有清理错误，StrategyStartupRollbackError通过多原因Unwrap保留启动、取消与各停止/平仓原因；全部清理仍会尝试，未启动项不被停止。
- 专用Funding Carry受管恢复准入只接受单一结构化剩余资产恢复结果及普通单原因包装；joined/multi-cause错误即使包含恢复诊断也不准入。原核账内容、独立封锁和资金claim证明要求保持不变，不新增自动平仓/处置或释放。
- 最新连续3轮race（根包3.787s/strategy2.803s）覆盖全部错误可追溯、先取消context再进入Stop、取消及旧回滚兼容、正常受管恢复与多原因拒绝。实际Funding Carry策略成功启动后注入UNKNOWN并取消，真实Stop返回自动平仓拒绝，管理器同时保留取消与该拒绝；无新下单/还款、未报告平仓完成。该夹具不等于完整构造器或真实账户验收。
- 根包/strategy最新vet、diff检查、Ruby9runs/58assertions、Yarn类型检查/45文件225项测试/Vite PWA构建均完成通过。初轮十包报告包含冻结步骤前的源码，仅作阶段记录，不将该结果继承为最终源码通过。
- 冻结后最终十包报告 `/private/tmp/quantmesh-trading-race-rc962-frozen-final/results.json` 与 `results.md` 已读取：1933 pass、8 MySQL skip、零失败，无缺包/解析错误，strategy143.336s；全部新增回滚/冻结/真实UNKNOWN关闭拒绝/多原因准入用例通过。source_commit=04ce8896、source_version=3.111.0-rc962、source_dirty=true，是本版提交前源码证据，不是同提交严格MySQL验收；测试期间最终生产实现未再变化。
- R09/R07的失败可观测性和取消顺序有所推进，但失败实例完整接管、资产处置、现时全账户库存归属、原子fencing及R01–R15其余验收仍未闭合。本版没有同提交严格MySQL验收，未合main、打tag、发布、部署或连接真实账户，不宣称实盘/盈利已验收。

## rc961：受管启动不能脱离调用方生命周期

- 实际构造器调用专用受管启动，但旧管理器 StartAll 使用创建管理器时的 background context。先以真实 Funding Carry 耐久读取回调取消调用方，复现旧路径仍导入历史余量，定向用例明确失败；这不是仅靠静态推测或新接口自测。
- 新 StartAllContext 把策略恢复与循环绑定调用方和管理器两个 context，启动前/每项调用前/每项返回后检查取消；未调用的策略不触发 Stop，已调用项和先前成功项走原回滚。旧 StartAll 仍绑定管理器生命周期，专用 Funding Carry 生产调用显式传入所属 runtime context。策略枚举失败保留原错误链，不以泛化错误丢掉取消原因。
- 最新三轮 race（根包4.667s/strategy3.413s）验证 nil/已取消/管理器停止/第一或第二项启动期间取消、先前已启动项全部回滚而尚未启动项不调用、调用方或管理器取消均通知循环、旧接口兼容，以及真实 Funding Carry 循环退出；真实余量恢复用例证明取消后不导入，完整耐久内容保持不变、无金融 RPC 或启动循环。实际Web异步入口使用background context，不把HTTP请求结束视为Bot生命周期结束。
- 此处仅闭合 R09 的调用生命周期缺口，不自动释放claim、不解除交易门禁，也不把退出循环当作平仓证据。构造器前置失败的受管接管、当前库存/资产处置、部分成交补偿、全账户归属、原子fencing、其他专用运行时调用方context绑定及R01–R15其余验收仍待完成。
- 十包race报告 `/private/tmp/quantmesh-trading-race-rc961-final/results.json` 和 `results.md` 已读取终态：1929 pass、8 MySQL skip、零失败，无缺包/解析错误，strategy145.031s、Web159.997s；source_commit=4051fe78、source_version=3.111.0-rc961、source_dirty=true，是提交前工作树证据，不是同提交严格数据库验收。随后补强第二项取消夹具的最新三轮结果如上，生产实现未再变化。
- 根包/strategy vet、diff检查、Ruby9runs/58assertions及Yarn类型检查、45文件/225项测试、Vite/PWA构建均已通过。不继承旧版严格MySQL结果；未改main、打tag、发布、部署或访问真实账户，不宣称完整恢复或盈利已验收。

## rc960：受管核账实例不能冒充正在交易

- 实际追踪发现 Bot List/GetBot 适配器的 running 表示注册表中有受管实例，而 Funding Carry 仪表盘直接把该值显示为运行；active_bots 还无条件累计配置数。新增 funding_carry_runtime 单独报告真实循环及待核账状态，不改变原受管停止/资本claim生命周期，列表和详情生产适配器都接入该读取。
- 仪表盘状态优先待核账，其次真实循环；受管但缺少策略证据返回 unknown，停止、待核账、未知都不计入活跃数。专用 status 接口的 running 改为已确认交易状态，同时显式返回 managed 以保留受管语义。React 使用真实国际化资源显示待核账/未知，缺少语言键沿现有 zh-CN fallback，不在 JSX 写死新增文案。
- 正常化状态不能代替资金证明：历史余量仍不等于现时库存，未增加恢复交易/自动处置/资金释放能力。真实构造器前置失败、全账户资产归属、部分成交处置、原子 fencing 与 R01–R15 其余风险仍待闭合。
- 实际剩余资产 Start 后，将真实策略管理器放入 BotManager 注册表并读回状态，证明保留 managed 且不报告交易循环；原资金释放拒绝断言保留。Web 三轮 race 覆盖状态优先级、专用 HTTP、真实仪表盘 HTTP 四类实例且仅1个计为活跃；根包5.655s、Web4.640s。这是夹具接线证据，不是完整生产构造器或浏览器端到端验收。
- 前端首轮新增服务端渲染夹具因导入 browser-only API 触发 window 未定义，已 mock 请求模块隔离环境后重跑；不修改生产请求模块以迁就测试。最新Yarn类型检查、45文件/225项测试、Vite/PWA构建通过，Ruby9runs/58assertions通过。根包/Web全量race完成（42.569s/77.237s），两包vet及diff检查通过；初次Go缓存访问权限失败的命令原样在允许环境重跑通过。结果是本版提交前工作树证据，不是十包或同提交严格 MySQL 验收。未合main、打tag、发布、部署或访问真实账户。

## rc959：已核清历史余量的受管核账启动分支

- 生产专用构造器调用受管启动准入：仅完整账本已导入、UNKNOWN/in-flight、未开始交易循环、无未保存金融意图/错误且账户/本金/成交证据再核验通过的结构化剩余资产恢复状态可继续构造SymbolRuntime。要求唯一Funding Carry策略及同一OpeningGate绑定，独立funding_carry_reconciliation_required封锁保留其他暂停，并标记平仓未核清；后续资本claim/租约移交走原受管生命周期。
- 结构化错误在钱包协调及释放成功后才返回，钱包释放失败不能因含恢复信息而被errors.As误认成准入；其他初始化/账本/归属错误仍返回失败。受管接管不表示当前余额可用、不重发下单或还款，不解除UNKNOWN，不把资金claim当作可释放。
- 管理器原enabled-only回退可将未启动Funding Carry显示为running；首个夹具因未配置Enabled而没复现此问题，已修正为显式启用并用真实旧接口回退路径对照。新增IsRunning按实际started/context报告未启动/正常循环/停止，不用配置开关冒充成功。
- 根包使用真实FundingCarry策略、管理器、完整schema6金融/成交快照和钱包锁隔离夹具验证生产准入函数；无真实交易所连接。全构造器前置权限/余额检查、注册表保留及Web实际渲染尚未端到端验收，预检失败/其他恢复类型的受管接管也不在本次通过范围；当前库存与剩余资产最终处置仍未闭合。
- 最终受管准入/真实运行报告/新旧恢复/原资金释放用例连续3轮race通过（根包3.511s、strategy3.328s），覆盖普通本金账本错误、错账户、取消、所有权丢失、钱包unlock失败及gate错绑定；根包夹具最初缺margin Borrow接口导致编译失败，改为完整margin接口后才接受业务验证。已确认状态数据含历史余量、reconciliation_required=true且IsRunning=false，原钱包释放真实调用fc.VerifyFlat并拒绝删除claim，未读实盘或发金融RPC。
- 十包 `/private/tmp/quantmesh-trading-race-rc959-final/results.json` 与 `results.md` 已读回1923 pass、8 MySQL skip、0失败、无缺包/解析错误，strategy140.102s；source_commit=778fbd32、source_version=3.111.0-rc959、source_dirty=true，是本版提交前源码回归，非同提交严格数据库验收。根包/strategy vet、diff检查、Ruby9runs/58assertions、Yarn类型检查/测试/Vite PWA构建通过。
- 不继承rc957数据库证据。R01–R15其余要求、全账户归属、当前库存/资产处置、部分成交/完整恢复、原子世代fencing及真实盈利证明仍待闭合，未改main、发布、部署或访问真实账户。

## rc958：重启接管剩余资产的完整历史账本

- 新增实际Start回归先复现rc957：耐久净量0.4008、确认消耗0.4，但普通恢复拒绝后内存没有账本，状态API返回qty=null/known=false，而非已核清的历史差额0.0008。
- 启动在已保存买回/成交/还款恢复之后，使用与其他路径一致的操作门→钱包锁，锁内重新读取完整schema/本金账本/成交费用/确认消耗，核验实际基础币及当前账户；context/运行所有权在内存接管前再次检查。本地未保存的还款ACK/买回CID或保存失败不被较旧耐久记录覆盖。
- 只接管已验证历史账本，深拷贝逐笔成交，保留其他腿、借款身份和金融事件，标记UNKNOWN/in-flight后返回仍需现时库存与处置核验的错误；重复Start可读回同样差额，不改耐久记录、不调用下单/还款、不宣告运行成功。零余量仍交给原普通恢复核验。
- 生产边界：funding_carry_runtime.go 在strategyManager.StartAll失败后直接返回nil，早于SymbolRuntime构造；本版真实启动错误可明确报告余量，策略对象状态读取已接管账本，但不宣称失败实例已挂入Web或可进行受管处置。失败后保留/重新挂入只允许核账的受管运行时仍需实现并验证，不能把内部可见性冒充完整恢复闭环。
- 资金释放接线只读复核：specialized Funding Carry 启动失败清理与停止释放都调用fc.VerifyFlat；不存在已举证的绕过该余量门禁路径。标准MSE资金释放要求私有账本核验和可取消库存能力，缺能力并不把GetPositions=nil当作空仓。此处不替代完整运行时资金释放演练或原子fencing验收。
- 十包 `/private/tmp/quantmesh-trading-race-rc958-final/results.json` 与 `results.md` 已读回1920 pass、8 MySQL skip、0失败，无缺包/解析错误，strategy140.086s，source_commit=53ef37fe、source_version=3.111.0-rc958、source_dirty=true；这是本版提交前生产源码回归，不是同提交严格MySQL验收。
- 随后仅补强测试夹具的UNKNOWN/in-flight、借款身份及启动诊断金额/现时库存边界断言，生产实现未变；最新新旧恢复、余量、启动还款和钱包锁序用例连续3轮 race 通过（3.068s），覆盖锁内快照更新/丢失/错误、错账户/资产/本金账本、取消/所有权丢失、本地未保存ACK/CID/保存失败拒绝覆盖、其他腿保留及真实输入深拷贝。strategy vet 初次缓存权限失败后原样允许环境重跑通过；diff检查、Ruby9runs/58assertions、Yarn类型检查/测试/Vite PWA构建通过。
- rc957同提交严格MySQL证据不继承为本版结果。当前失败实例的受管Web接管、实物库存覆盖、全账户其他所有者归属、剩余资产处置、部分成交/完整恢复、原子世代fencing及R01–R15其余要求仍待闭合；未访问生产库/账户、发布、部署或验收盈利。

## rc957：同提交严格 MySQL 验证检查点

- 测试代码提交：`e6d1260f0b92d42b12a1eb15ea9c158c62b542ce`，版本 `3.111.0-rc957`。测试前后 HEAD 完全相同，tracked diff 与 index diff 均为空；报告 source_dirty=true 仅因既有无关 `?? --help/`，未读取、改动或纳入提交。
- 命令：`ruby scripts/verify_trading_race.rb /private/tmp/quantmesh-trading-race-rc957-same-commit-mysql --require-mysql`。测试 DSN 和 destructive-schema 许可仅指向本轮新建的 `quantmesh_readiness_rc957`，开始前确认业务表数为0，未读取生产配置。
- 环境：MySQL `8.0.36`，镜像 `sha256:a532724022429812ec797c285c1b540a644c15e248579c6bfdf12a8fbaab4964`；容器 `quantmesh-readiness-mysql-rc957`，精确ID `6716596b4145477d6eb92ab753a491527af03558cfa599b27f874be0d4aa0a5f`，标签 isolated-readiness-test。无宿主机数据卷，512MiB tmpfs、1GiB内存/2CPU，仅监听 `127.0.0.1:32779`；清理前再次核对身份及挂载。
- JSON 与 Markdown 已读回1925 pass、0 skip、0 fail，无缺包/解析错误，mysql_required=true、missing_verified_mysql_cases为空；8项强制用例逐一pass：独立暂停所有者、资金费身份/覆盖、账户钱包预留、借贷利息账本、成交覆盖迁移、现货库存快照、歧义Bot归属回填拒绝、利润提取规则。strategy140.776s。
- 容器和tmpfs库已精确删除，并读回无残留；测试数据不可恢复，报告 `/private/tmp/quantmesh-trading-race-rc957-same-commit-mysql/results.json`、`results.md` 和镜像保留。本检查点只更新3份文档，不修改代码或提升版本。
- 本次补齐rc957的本地同提交数据库证据，不替代目标平台交付、生产迁移、真实账户资产覆盖或盈利证明。当前实际Start会保留耐久余量证据并拒绝启动，但没有完整重启接管/处置；全账户库存归属、部分成交恢复、原子世代fencing及R01–R15其余要求仍待闭合。未改main、打发布标签、部署或实盘操作。

## rc957：还款后剩余买回资产独立核账

- 实际 `closeReverse` 回归先复现旧行为：净买回0.4008、确认还款0.4之后返回成功并宣告平仓，遗漏0.0008；不是只从提交标题推断风险。
- 本版使用既有持久化逐笔费用/净量和实际确认还款消耗，按十进制有理数独立计算 margin 剩余量，不用交易精度或4 ULP容差抹掉正差额。还款记录和本金清零保留，但剩余操作继续未解决，不重复RPC，不把margin差额写入spot库存。
- 空仓证明同时核对耐久与内存记录；普通恢复拒绝遗留差额，恢复专用解码仍可读取历史证据。状态 API 返回精确字符串/known/basis，未知为null，未建立所有权的启动失败状态不虚构零余额。
- 有效零差额保持可恢复/可核验；重复订单、错账户/资产、未核清成交、超耗（含很小的负差额）、缺/无效/重复还款证据等不能返回已知零。多周期不同来源精确求和，微量正差额同时覆盖内存、耐久与实际Start。原来的有余量成功预期改为保留未解决资产；补充真实零余量成功用例，并用明确base fee构造零余量夹具，让原微债务/最终所有权门禁仍被真实执行而非提前挡住。
- 最终重点关联 race 两轮111.344s通过；`go vet ./strategy`、`git diff --check`、Ruby门禁9 runs/58 assertions、Yarn typecheck/测试/Vite PWA构建均通过。初轮扩大回归不算验收：首轮有包级/HTTP用例失败，原样具名HTTP重跑通过但原因未完全确认；随后完整回归揭示3处旧有余量成功预期，逐一调整夹具与语义后再跑。
- 最终十包 `/private/tmp/quantmesh-trading-race-rc957-final3/results.json` 与 `results.md` 已读回1917 pass、8 MySQL skip、0失败、无缺包/解析错误，strategy140.019s，source_version=3.111.0-rc957、source_commit=74805846、source_dirty=true。这是当前提交前源码回归，不是本版同提交严格MySQL结果；此前final/final2失败不作为通过证据，rc956数据库证据不继承为rc957验收。
- 这是可审计历史差额核算与空仓漏洞修复，不等于当前资产仍在账户或可支用，也不是自动补偿/出售/转账能力。重启后残余资产的完整接管、全账户库存归属/现时覆盖和最终处置仍待闭合；R01–R15范围不缩减，未发布、部署、访问真实账户或盈利验收。

## rc956：同提交严格 MySQL 验证检查点

- 测试代码提交：`e0e2f2f0cb337796d0dfd14165d3fff260df52fe`，版本 `3.111.0-rc956`；测试前后 HEAD 完全相同，tracked diff 与 index diff 均为空。报告 source_dirty=true 仅因原有无关 `?? --help/`，未读取或改动该目录，不把它纳入提交。
- 命令：`ruby scripts/verify_trading_race.rb /private/tmp/quantmesh-trading-race-rc956-same-commit-mysql --require-mysql`。只对本次新建且初始业务表数为0的一次性 schema `quantmesh_readiness_rc956` 设置测试 DSN 和 destructive-schema 显式许可，没有读取生产配置。
- 环境：MySQL `8.0.36`，镜像 `sha256:a532724022429812ec797c285c1b540a644c15e248579c6bfdf12a8fbaab4964`；容器 `quantmesh-readiness-mysql-rc956`，ID `14a01ace1b2d445a2ea20a7b7f0dd398e969aaad02bfefb076760dc44990441b`。启动/清理前核对 `isolated-readiness-test` 标签和精确身份，无宿主机数据卷，`/var/lib/mysql` 为512MiB tmpfs，内存1GiB/CPU2，仅监听 `127.0.0.1:32778`。
- JSON 与 Markdown 已读回1921 pass、0 skip、0 fail、无缺包/解析错误，mysql_required=true、missing_verified_mysql_cases为空；全部8项强制MySQL用例各自为pass，覆盖暂停所有者、资金费身份/覆盖、账户预留、借贷利息账本、成交覆盖迁移、现货库存快照、歧义Bot归属回填拒绝和提取规则。
- 临时容器和tmpfs库已删除，并读回精确ID无残留；保留镜像及 `/private/tmp/quantmesh-trading-race-rc956-same-commit-mysql/results.json`、`results.md`。本检查点仅更新文档，不修改代码或提升版本。
- 此项补齐当前代码的本地同提交数据库回归，不代表生产迁移、目标平台交付、真实账户归属/恢复或净盈利验收。R01–R15范围不缩减；全账户库存、剩余资产、部分成交/完整恢复、原子世代fencing及真实盈利证据仍待闭合，未合入main、未打发布标签、未部署或实盘操作。

## rc956：空仓核验与恢复统一锁顺序

- 真实 VerifyFlat 隔离回归观察到它在等待已被持有的策略操作门时先获取钱包租约，导致操作持有者无法取得钱包（探测请求超时）；启动恢复、tick 与手动关闭则为操作门后钱包锁，形成反向锁序。
- VerifyFlat 先取得可取消操作门，再进入既有钱包协调租约；不修改全账户资本核验外层钱包租约协议、不放宽空仓/持久化/UNKNOWN门禁。回归核验等待操作期间钱包可由当前操作持有者取得，取消后操作门和钱包仍可正常重试。
- 补充回归复现仅调整锁序后，已失去所有权的调用仍会排队至超时；保留取操作门前的所有权快速拒绝，钱包协调内部仍二次核验。前轮最终测试启动于该补充修正前，不作为当前源码的最终证据。
- 全账户库存归属、剩余资产、完整恢复、严格数据库/发布证据及原子世代 fencing 仍未闭合；此批只消除该实际锁序冲突，不宣称所有并发路径已验收。
- 验证：锁序/取消后重试、同钱包串行化及所有权快速拒绝五轮 race 通过（2.978s）；最终关联策略/解码/关闭路径两轮 race 通过（251.878s）。strategy vet、diff 检查和 Ruby 门禁9项/58断言通过；前端 yarn verify 类型检查、44文件224测试及 Vite/PWA 构建通过。此前3.141s定向及250.750s关联结果不包含所有权补充修正，不替代最终验证。
- 最终十包 `/private/tmp/quantmesh-trading-race-rc956-final2/results.json` 与 `results.md` 已读回1913 pass、8 MySQL skip、0失败、无缺包/解析错误，source_version=3.111.0-rc956、source_commit=5739e73a、source_dirty=true；前轮 final 的1912项为补充修正前证据，非最终结果。这是提交前源码回归，不是本版同提交严格 MySQL 验证。未访问真实账户/生产库、未下单或发布。

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
