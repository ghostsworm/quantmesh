# 接送/到达协同 SAAS 方案（B1：国际客版）
>
> **定位**：国际客为主的旅游城市（如清迈）；酒店/公寓/民宿；**按单计费**；纯软件。  
> **核心差异**：沟通链路以 **WhatsApp / Email / OTA 站内消息** 为主；“等待点指引（含地图链接）+ 多语言模板 + 弱网兜底 + 证据留痕”更重要。  
>
> 本文可视为 `老挝酒店接送机调度SAAS方案_A1.md` 的国际化版本：同一套状态机与留痕逻辑，但字段、话术与落地方式按国际客场景调整。

---

## 目录

- [1. 适用场景与价值主张（国际客）](#1-适用场景与价值主张国际客)
- [2. B1 的“可交付边界”](#2-b1-的可交付边界)
- [3. 角色与流程（B1）](#3-角色与流程b1)
- [4. 客人端表单字段（B1 推荐）](#4-客人端表单字段b1-推荐)
- [5. 司机端与酒店端（B1 的关键 UI）](#5-司机端与酒店端b1-的关键-ui)
- [6. 消息模板（EN 为主，可选中文）](#6-消息模板en-为主可选中文)
- [7. 异常场景 SOP（国际客常见）](#7-异常场景-sop国际客常见)
- [8. 弱网与“客人落地无网”兜底](#8-弱网与客人落地无网兜底)
- [9. 定价与试点（按单）](#9-定价与试点按单)
- [10. 合规与隐私（国际客常见关注点）](#10-合规与隐私国际客常见关注点)

---

## 1. 适用场景与价值主张（国际客）

国际客场景里，酒店提供/代订接送的核心风险是：
- 客人语言不通，落地找不到人/找不到车 → 差评
- 航班延误/更改 → 司机白等/客人投诉
- 沟通渠道分散（OTA 站内消息、WhatsApp、Email）→ 前台被反复问

**价值主张**（对酒店说人话）：
- Reduce missed pickups and guest complaints
- Save front desk time with a standardized workflow
- Dispute-proof logs with timestamps (who did what, when)

---

## 2. B1 的“可交付边界”

B1 强调“先跑起来”的可交付：
- ✅ 不要求对接 PMS/Channel Manager/OTA 官方 API
- ✅ 通过 OTA 预抵达消息/Email/WhatsApp 发送链接即可闭环
- ✅ 支持多语言模板（至少英文；可选中文）
- ✅ 保留 A1 的状态机、审计日志、导出对账

B1 暂不做（可作为后续增强）：
- ❌ 自动派单/路线优化
- ❌ 航班自动跟踪（需要第三方数据源与稳定性）
- ❌ 自动发送 WhatsApp/SMS（可先做“复制模板”）

---

## 3. 角色与流程（B1）

### 3.1 角色
- Front desk / Reservations：派单、处理异常
- Manager / Owner：看报表与对账
- Driver：4 步状态按钮
- Guest：填表 + 获取等待点指引 + 取车码核验

### 3.2 流程（不集成也能跑）
1. 酒店在 **pre-arrival message**（OTA/Email/WhatsApp）里发送登记链接
2. 客人填表（WhatsApp/Email 为主）
3. 酒店看板派单 → 司机确认 → 到达 → 接到 → 完成
4. 全程时间线留痕 + 导出对账

---

## 4. 客人端表单字段（B1 推荐）

> 原则：国际客更依赖 WhatsApp 与明确地点指引；字段要“够用但不重”。

### 4.1 必填
- **Full name**（英文名）
- **WhatsApp number**（含国家码，例如 +66…）
- **Flight number**（例如 TGxxx）
- **Arrival date**
- **Arrival time**（本地时间；如果客人不确定可选“unknown”）
- **Passengers**（总人数）
- **Destination**（hotel name/branch，默认预填）

### 4.2 推荐（可选，但强烈降低事故）
- **Language preference**（English / 中文 / Other）
- **Luggage**（0/1/2/3+ 或 “big luggage: yes/no”）
- **Special requests**（child seat / wheelchair / elderly / etc.）
- **Email**（作为备份联系渠道）

### 4.3 系统自动生成
- **Pickup code（4 digits）**：跨语言核验最有效
- **Meeting point**：展示固定文字 + 图片 + **Google Maps link**

---

## 5. 司机端与酒店端（B1 的关键 UI）

### 5.1 酒店端（必须可“快速复制指引”）
- 任务卡片一眼看到：Flight、ETA、WhatsApp、Pickup code、Meeting point link
- 一键复制：
  - 给客人的英文模板（含地图链接、车牌、取车码）
  - 给司机的英文模板（含客人 WhatsApp、取车码、等待点）
- 显示 **Last driver sync time**（司机弱网常见）

### 5.2 司机端（极简 + 弱网）
4 个按钮：
- Accept
- Arrived
- Picked up（建议输入 pickup code）
- Completed

显示：
- Guest name + WhatsApp + pickup code
- Meeting point link（地图）

---

## 6. 消息模板（EN 为主，可选中文）

> B1 可以先做“复制模板”，无需自动发送。

### 6.1 Pre-arrival message（发给客人：收集信息）

**EN**
> For airport pickup/transfer, please fill in this short form at least 24 hours before arrival: <link>.  
> After submission, you will receive a pickup code and meeting point instructions.

（可选中文）
> 如需接机/接送，请至少提前24小时填写：<link>。提交后会收到取车码与集合点指引。

### 6.2 Assigned message（派单后给客人：告诉他怎么找车）

**EN**
> Hi <GuestName>, this is your hotel pickup driver.  
> Car plate: <Plate>. Meeting point: <MeetingPointText>. Map: <GoogleMapsLink>.  
> Please reply with your pickup code <1234> when you arrive.

### 6.3 Driver instruction（派单后给司机）

**EN**
> Pickup: <GuestName>, flight <FlightNo>, ETA <Time>. Passengers: <N>. Pickup code: <1234>.  
> Guest WhatsApp: <Number>. Meeting point: <Text>. Map: <Link>.  
> If issues: mark exception in system and contact front desk.

---

## 7. 异常场景 SOP（国际客常见）

建议最小覆盖：
- Flight delayed / time changed（前台更新 ETA，留痕并提醒司机）
- Cannot find guest（司机点异常 → 前台复制模板让客人发定位/照片）
- Guest cannot find driver（前台转发地图链接 + 车牌 + 等待点照片）
- No-show（记录等待开始/结束时间）
- Reassign driver（转派留痕）

国际客“找不到人”时，最有效的两条动作是：
- **Ask for a photo of their current location / exit sign**
- **Send the exact map pin + a photo of the meeting point**

---

## 8. 弱网与“客人落地无网”兜底

国际客落地后也可能无网/漫游关闭，因此要做到：
- **Pre-arrival message 里提前写清 meeting point**（文字 + 图片）
- **pickup code** 必须可离线使用（客人报码即可核验）
- 司机端支持离线队列，允许延迟同步（最终一致）

---

## 9. 定价与试点（按单）

建议仍采用三档（结合当地币种报价）：
- Basic / Standard / Pro

试点建议：
- 14 天
- 明确计费口径：以 `COMPLETED` 作为计费单（或 `PICKED_UP`，二选一）
- 试点结束交付：异常率、等待时长、司机响应时延、可导出的对账明细

---

## 10. 合规与隐私（国际客常见关注点）

建议在客人表单页与酒店合同里明确：
- 数据用途：仅用于安排接送
- 数据保留：例如 180 天（可协商）
- 导出与删除：酒店可导出；终止后提供导出并删除/匿名化

如果首站在泰国，后续建议关注当地数据保护要求（例如 PDPA），但 MVP 阶段至少做到：
- 最小化采集
- 权限隔离
- 审计留痕

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
