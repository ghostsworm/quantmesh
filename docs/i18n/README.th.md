<div align="center">
  <img src="../../logo/qm_thick_tail_white.svg" alt="QuantMesh Logo" width="200"/>
  
  # QuantMesh Market Maker
  
  **ระบบ Market Maker Crypto ความถี่สูง**

  [![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue.svg)](https://golang.org/dl/)
  [![License](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](../../LICENSE)
  [![GitHub Stars](https://img.shields.io/github/stars/ghostsworm/quantmesh.svg?style=social&label=Stars)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Forks](https://img.shields.io/github/forks/ghostsworm/quantmesh.svg?style=social&label=Forks)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Issues](https://img.shields.io/github/issues/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/issues)
  [![GitHub Release](https://img.shields.io/github/release/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/releases)
  [![Website](https://img.shields.io/badge/Website-quantmesh.io-green.svg)](https://quantmesh.io)
  
  [简体中文](../../README.md) | [繁體中文](README.zh-TW.md) | [English](README.en.md) | [Español](README.es.md) | [Français](README.fr.md) | [Português](README.pt.md) | [ไทย](README.th.md)
</div>

---

## 🎯 ทำไมต้องเลือก QuantMesh?

| คุณสมบัติ | QuantMesh | โซลูชันอื่น |
|---------|-----------|----------------|
| **รองรับ Exchange** | 20+ แพลตฟอร์ม | โดยปกติ 3-5 แพลตฟอร์ม |
| **ความหน่วงในการตอบสนอง** | ระดับมิลลิวินาที | ระดับวินาที |
| **การควบคุมความเสี่ยง** | การควบคุมแบบหลายชั้น | การควบคุมพื้นฐาน |
| **ทดสอบในสภาพจริง** | ปริมาณการซื้อขาย $100M+ | ยังไม่ทดสอบ |
| **อินเทอร์เฟซเว็บ** | ✅ UI React แบบสมบูรณ์ | ❌ ไม่มี/พื้นฐาน |
| **โอเพ่นซอร์ส** | AGPL-3.0 | โค้ดปิด/จำกัด |
| **ข้อมูลแบบเรียลไทม์** | WebSocket เท่านั้น | REST polling |
| **การทำงานพร้อมกัน** | 1000+ คำสั่ง/วินาที | จำกัด |

**ข้อดีหลัก:**
- ✅ **ผ่านการทดสอบ**: พิสูจน์แล้วด้วยปริมาณการซื้อขาย $100M+
- ✅ **ประสิทธิภาพสูง**: ความหน่วงต่ำกว่า 10ms ด้วยสถาปัตยกรรม WebSocket
- ✅ **ครอบคลุม**: โซลูชันสมบูรณ์ตั้งแต่การซื้อขายจนถึงการตรวจสอบ
- ✅ **โปร่งใส**: โอเพ่นซอร์สเต็มรูปแบบ โค้ดตรวจสอบได้
- ✅ **ขยายได้**: ระบบปลั๊กอินสำหรับการปรับแต่ง

---

## 📊 Telemetry

**Third-party analytics (PostHog) has been removed.** The Go backend and install script no longer send usage events. Symbols in `utils/telemetry.go` are no-ops for compatibility.

The Web UI may still load a **1×1 pixel** on startup (`webui/src/services/telemetry.ts`). Disable with `VITE_DISABLE_TELEMETRY=1` in `webui/.env.local` or `localStorage` key `QUANTMESH_DISABLE_TELEMETRY` = `1`.

See [TELEMETRY_GUIDE.md](../../docs/TELEMETRY_GUIDE.md).
---

## ⚠️ ข้อจำกัดความรับผิดชอบ

ซอฟต์แวร์นี้มีไว้เพื่อการศึกษาและการวิจัยเท่านั้น การซื้อขายสกุลเงินดิจิทัลมีความเสี่ยงสูงและอาจส่งผลให้สูญเสียเงินทุน
- ผู้ใช้รับผิดชอบแต่เพียงผู้เดียวต่อผลกำไรหรือขาดทุนใดๆ จากการใช้ซอฟต์แวร์นี้
- ทดสอบอย่างละเอียดบน Testnet เสมอก่อนใช้เงินจริง
- นักพัฒนาไม่รับผิดชอบต่อความสูญเสียเนื่องจากบั๊กซอฟต์แวร์ ความหน่วงของเครือข่าย หรือความล้มเหลวของ exchange

## 🪙 รองรับการชำระเงินด้วย Crypto

QuantMesh รองรับการชำระเงินด้วยสกุลเงินดิจิทัลสำหรับการสมัครสมาชิกและใบอนุญาต:

### สกุลเงินดิจิทัลที่รองรับ
- **BTC** (Bitcoin)
- **ETH** (Ethereum)
- **USDT** (Tether, ERC20)
- **USDC** (USD Coin, ERC20)

### วิธีการชำระเงิน
1. **Coinbase Commerce** (แนะนำ)
   - ยืนยันอัตโนมัติ
   - รองรับหลายสกุลเงินดิจิทัล
   - หน้าชำระเงินที่ง่าย

2. **การชำระเงินด้วย Wallet โดยตรง**
   - ไม่มีบุคคลที่สามเกี่ยวข้อง
   - ความเป็นส่วนตัวมากขึ้น
   - ยืนยันด้วยตนเอง (1-24 ชั่วโมง)

### เริ่มต้นอย่างรวดเร็ว
```bash
# วิธี A: Coinbase Commerce (15 นาที)
# 1. ลงทะเบียนที่ https://commerce.coinbase.com
# 2. กำหนดค่า API Key ใน .env.crypto
# 3. เริ่มบริการ

# วิธี B: Wallet โดยตรง (5 นาที)
# 1. กำหนดค่าที่อยู่ wallet
# 2. เริ่มบริการ
# 3. ยืนยันด้วยตนเอง
```

### เอกสาร
- 📖 [คู่มือการชำระเงินผู้ใช้](../CRYPTO_PAYMENT_GUIDE.md)
- 🚀 [คู่มือเริ่มต้นอย่างรวดเร็ว](../CRYPTO_PAYMENT_QUICKSTART.md)
- 🔧 [คู่มือการตั้งค่า](../CRYPTO_PAYMENT_SETUP.md)
- 📊 [สรุปการใช้งาน](../reports/CRYPTO_PAYMENT_SUMMARY.md)

### ทำไมต้องชำระเงินด้วย Crypto?
✅ ไม่ต้องใช้บัตรเครดิตหรือบัญชีธนาคาร  
✅ เข้าถึงได้ทั่วโลก ไม่มีข้อจำกัดตามภูมิภาค  
✅ ค่าธรรมเนียมการทำธุรกรรมต่ำกว่า (1% เทียบกับ 2.9%)  
✅ การปกป้องความเป็นส่วนตัวที่ดีขึ้น  
✅ ยืนยันอย่างรวดเร็ว (10-30 นาที)  
✅ เหมาะสมอย่างสมบูรณ์แบบสำหรับซอฟต์แวร์ซื้อขาย crypto  

## 📜 สัญญาอนุญาต

โปรเจกต์นี้ใช้ **โมเดลสัญญาอนุญาตแบบคู่**:

### สัญญาอนุญาตโอเพ่นซอร์ส AGPL-3.0
- ✅ ใช้ แก้ไข และแจกจ่ายได้ฟรี
- ⚠️ **งานที่ได้มาใหม่ทั้งหมดต้องเป็นโอเพ่นซอร์ส** และเผยแพร่ภายใต้ AGPL-3.0
- ⚠️ ต้องให้ซอร์สโค้ดแม้สำหรับบริการเครือข่าย
- ⚠️ โค้ดที่แก้ไขต้องมีส่วนร่วมกลับไปยังชุมชน

### สัญญาอนุญาตเชิงพาณิชย์
หากคุณต้องการใช้ซอฟต์แวร์นี้ในแอปพลิเคชันหรือบริการที่เป็นกรรมสิทธิ์ หรือไม่ต้องการทำให้การแก้ไขของคุณเป็นโอเพ่นซอร์ส คุณต้องซื้อสัญญาอนุญาตเชิงพาณิชย์

**ขอบเขตสัญญาอนุญาตเชิงพาณิชย์:**
- ใช้ในแอปพลิเคชันที่เป็นกรรมสิทธิ์
- ไม่มีภาระผูกพันในการทำให้การแก้ไขเป็นโอเพ่นซอร์ส
- รวมเข้ากับผลิตภัณฑ์ที่เป็นกรรมสิทธิ์เพื่อการแจกจ่าย
- การสนับสนุนทางเทคนิคและอัปเดตแบบลำดับความสำคัญ

**สอบถามสัญญาอนุญาตเชิงพาณิชย์:**
- 📧 อีเมล: contact@quantmesh.io
- 🌐 เว็บไซต์: https://quantmesh.io/commercial

---

### รายละเอียดสัญญาอนุญาต

โปรเจกต์นี้มีสัญญาอนุญาตแบบคู่ภายใต้:

1. **AGPL-3.0 (GNU Affero General Public License v3.0)**
   - ใช้ แก้ไข และแจกจ่ายได้ฟรี
   - งานที่ได้มาใหม่ทั้งหมดต้องเป็นโอเพ่นซอร์สภายใต้ AGPL-3.0
   - ต้องให้ซอร์สโค้ดแก่ผู้ใช้ทั้งหมด แม้สำหรับบริการเครือข่าย
   - การแก้ไขต้องมีส่วนร่วมกลับไปยังชุมชน

2. **สัญญาอนุญาตเชิงพาณิชย์**
   - จำเป็นสำหรับการใช้งานที่เป็นกรรมสิทธิ์
   - ไม่มีภาระผูกพันในการทำให้การแก้ไขเป็นโอเพ่นซอร์ส
   - รวมการสนับสนุนและอัปเดตแบบลำดับความสำคัญ

สำหรับการสอบถามสัญญาอนุญาตเชิงพาณิชย์ โปรดติดต่อ:
- 📧 อีเมล: contact@quantmesh.io
- 🌐 เว็บไซต์: https://quantmesh.io/commercial

## 🤝 การมีส่วนร่วม

เรายินดีต้อนรับการมีส่วนร่วม! นี่คือวิธีที่คุณสามารถช่วยได้:

- ⭐ **ให้ดาว repo นี้** หากคุณพบว่ามีประโยชน์
- 🍴 **Fork และใช้** โปรเจกต์
- 🐛 **รายงานบั๊ก** ผ่าน [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💡 **แนะนำฟีเจอร์** ผ่าน [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📝 **ส่ง PR** สำหรับการปรับปรุง
- 📖 **ปรับปรุงเอกสาร**

**หมายเหตุ:** ตามสัญญาอนุญาต AGPL-3.0 การมีส่วนร่วมทั้งหมดในโปรเจกต์นี้จะถูกเผยแพร่ภายใต้สัญญาอนุญาต AGPL-3.0 เดียวกัน

ดู [CONTRIBUTING.md](../CONTRIBUTING.md) สำหรับแนวทางโดยละเอียด

## 🙏 การขอบคุณ

ขอบคุณโปรเจกต์เดิม [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker) โดย [dennisyang1986](https://github.com/dennisyang1986) สำหรับการมีส่วนร่วมโอเพ่นซอร์ส ซึ่งให้พื้นฐานที่มั่นคงสำหรับโปรเจกต์นี้ สำหรับข้อมูลเพิ่มเติม โปรดดูที่ไฟล์ [NOTICE](../../NOTICE)

---

## 📞 ติดต่อและการสนับสนุน

- 🌐 **เว็บไซต์**: https://quantmesh.io
- 📧 **อีเมล**: contact@quantmesh.io
- 💬 **Discord**: [เข้าร่วมชุมชนของเรา](https://discord.gg/YOUR_INVITE_LINK)
- 🐛 **Issues**: [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💬 **การสนทนา**: [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📖 **เอกสาร**: [เอกสารแบบสมบูรณ์](../)

---

<div align="center">
  <strong>สร้างด้วย ❤️ โดยทีม QuantMesh</strong><br/>
  <sub>หากคุณพบว่าโปรเจกต์นี้มีประโยชน์ โปรดพิจารณาให้ ⭐</sub>
</div>

Copyright © 2025 QuantMesh Team. All Rights Reserved.

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
