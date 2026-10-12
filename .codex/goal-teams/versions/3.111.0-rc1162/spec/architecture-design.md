# RC1162 架构设计：生产核账组合与迁移协调

## 生产组合边界

沿用 `main.go` 初始化的 `StorageService`、`buildProductionOrderReconciliationService`、`web.SetOrderReconciliationService` 与受保护路由。组合测试应调用可复用的生产 wiring seam，而不是复制依赖装配或只测 handler。SQL repository 与只读证据仍可用；`Accounting`、`Gate` 故意不安装。

## 迁移协调要求

现有基础表通过嵌入 migration 创建，旧表补列另走 `columnExists` + `ALTER TABLE`。协调策略须满足：

- SQLite 和 MySQL 使用各自可验证的并发协调语义。
- 如果用数据库 advisory lock，应覆盖 schema 创建及补列全过程、约束获取超时、在所有出口释放，并验证锁连接生命周期；若连接池会换连接，不能使用绑定单连接的 lock 却在另一连接 unlock。
- 如果依靠原生 DDL 竞态容忍，必须准确识别“目标列已由竞争者创建”这一种错误，并重新读回 schema；不得把权限、磁盘、连接或语法等迁移错误当作成功。
- MySQL DDL 可能隐式提交，因此应用事务不能被误认为保护多个 ALTER 的原子性。

不预设具体实现；由后端实现、Reviewer 按 SQLite/MySQL 实际行为审查。迁移失败时 service 不注册，Gate 不释放。
