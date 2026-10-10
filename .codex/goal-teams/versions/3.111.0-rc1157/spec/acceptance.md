# 验收记录

当前尚未实现。用户已批准扩展策略级权威 durable cursor readback 和显式人工幂等 reconciliation；在各生产策略均接入并可重启读回之前，不可暴露可成功完成的解除开仓 gate 路径。只读核账案或 UI 不能标记此任务完成。

待实现和验证。每个通过结论必须绑定最终源码状态，并分别记录 API、durable storage、runtime gate、前端 i18n、测试、独立 Review，以及本地/提交/远端/发布状态。

不得用人工声明、静态读码、helper 单测、`git diff --check`、版本号或测试子集宣称人工核账/解锁生产闭环完成；不得声称真实实盘或盈利已经验收。
