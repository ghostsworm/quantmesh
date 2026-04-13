<div align="center">
  <img src="../../logo/qm_thick_tail_white.svg" alt="QuantMesh Logo" width="200"/>
  
  # QuantMesh Market Maker
  
  **고주파 암호화폐 마켓 메이커**

  [![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue.svg)](https://golang.org/dl/)
  [![License](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](../../LICENSE)
  [![GitHub Stars](https://img.shields.io/github/stars/ghostsworm/quantmesh.svg?style=social&label=Stars)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Forks](https://img.shields.io/github/forks/ghostsworm/quantmesh.svg?style=social&label=Forks)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Issues](https://img.shields.io/github/issues/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/issues)
  [![GitHub Release](https://img.shields.io/github/release/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/releases)
  [![Website](https://img.shields.io/badge/Website-quantmesh.io-green.svg)](https://quantmesh.io)
  
  [简体中文](../../README.md) | [繁體中文](README.zh-TW.md) | [English](README.en.md) | [Español](README.es.md) | [Français](README.fr.md) | [Português](README.pt.md) | [한국어](README.ko.md)
</div>

---

## 🎯 QuantMesh를 선택하는 이유

| 기능 | QuantMesh | 기타 솔루션 |
|---------|-----------|----------------|
| **거래소 지원** | 20+ 거래소 | 일반적으로 3-5개 |
| **응답 지연 시간** | 밀리초 수준 | 초 수준 |
| **위험 관리** | 다층 활성 제어 | 기본 제어 |
| **프로덕션 테스트** | $100M+ 거래량 | 테스트되지 않음 |
| **웹 인터페이스** | ✅ 완전한 React UI | ❌ 없음/기본 |
| **오픈 소스** | AGPL-3.0 | 클로즈드 소스/제한됨 |
| **실시간 데이터** | WebSocket 전용 | REST 폴링 |
| **동시성** | 1000+ 주문/초 | 제한됨 |

**주요 장점:**
- ✅ **전투 테스트**: $100M+ 거래량으로 입증됨
- ✅ **고성능**: WebSocket 아키텍처로 10ms 미만 지연 시간
- ✅ **포괄적**: 거래부터 모니터링까지 완전한 솔루션
- ✅ **투명성**: 완전 오픈 소스, 감사 가능한 코드
- ✅ **확장 가능**: 사용자 정의를 위한 플러그인 시스템

---

## 📊 Telemetry

**Third-party analytics (PostHog) has been removed.** The Go backend and install script no longer send usage events. Symbols in `utils/telemetry.go` are no-ops for compatibility.

The Web UI may still load a **1×1 pixel** on startup (`webui/src/services/telemetry.ts`). Disable with `VITE_DISABLE_TELEMETRY=1` in `webui/.env.local` or `localStorage` key `QUANTMESH_DISABLE_TELEMETRY` = `1`.

See [TELEMETRY_GUIDE.md](../../docs/TELEMETRY_GUIDE.md).
---

## ⚠️ 면책 조항

이 소프트웨어는 교육 및 연구 목적으로만 사용됩니다. 암호화폐 거래는 높은 위험을 수반하며 자본 손실을 초래할 수 있습니다.
- 사용자는 이 소프트웨어 사용으로 인한 모든 이익 또는 손실에 대해 전적으로 책임을 집니다.
- 실제 자금을 사용하기 전에 항상 테스트넷에서 철저히 테스트하세요.
- 개발자는 소프트웨어 버그, 네트워크 지연 시간 또는 거래소 장애로 인한 손실에 대해 책임을 지지 않습니다.

## 🪙 암호화폐 결제 지원

QuantMesh는 구독 및 라이선스를 위한 암호화폐 결제를 지원합니다:

### 지원 암호화폐
- **BTC** (Bitcoin)
- **ETH** (Ethereum)
- **USDT** (Tether, ERC20)
- **USDC** (USD Coin, ERC20)

### 결제 방법
1. **Coinbase Commerce** (권장)
   - 자동 확인
   - 여러 암호화폐 지원
   - 쉬운 결제 페이지

2. **직접 지갑 결제**
   - 제3자 개입 없음
   - 더 많은 개인정보 보호
   - 수동 확인(1-24시간)

### 빠른 시작
```bash
# 방법 A: Coinbase Commerce (15분)
# 1. https://commerce.coinbase.com에서 등록
# 2. .env.crypto에서 API 키 구성
# 3. 서비스 시작

# 방법 B: 직접 지갑 (5분)
# 1. 지갑 주소 구성
# 2. 서비스 시작
# 3. 수동 확인
```

### 문서
- 📖 [사용자 결제 가이드](../CRYPTO_PAYMENT_GUIDE.md)
- 🚀 [빠른 시작 가이드](../CRYPTO_PAYMENT_QUICKSTART.md)
- 🔧 [설정 가이드](../CRYPTO_PAYMENT_SETUP.md)
- 📊 [구현 요약](../reports/CRYPTO_PAYMENT_SUMMARY.md)

### 암호화폐 결제를 선택하는 이유?
✅ 신용 카드 또는 은행 계좌 불필요  
✅ 전 세계 접근성, 지역 제한 없음  
✅ 낮은 거래 수수료(1% vs 2.9%)  
✅ 더 나은 개인정보 보호  
✅ 빠른 확인(10-30분)  
✅ 암호화폐 거래 소프트웨어에 완벽하게 적합  

## 📜 라이선스

이 프로젝트는 **듀얼 라이선스 모델**을 사용합니다:

### AGPL-3.0 오픈 소스 라이선스
- ✅ 사용, 수정 및 배포 무료
- ⚠️ **모든 파생 작품은 오픈 소스화**되어야 하며 AGPL-3.0 하에 릴리스되어야 합니다
- ⚠️ 네트워크 서비스의 경우에도 소스 코드를 제공해야 합니다
- ⚠️ 수정된 코드는 커뮤니티에 기여되어야 합니다

### 상업용 라이선스
이 소프트웨어를 독점 애플리케이션이나 서비스에서 사용해야 하거나 수정 사항을 오픈 소스화하고 싶지 않은 경우 상업용 라이선스를 구매해야 합니다.

**상업용 라이선스 범위:**
- 독점 애플리케이션에서 사용
- 수정 사항을 오픈 소스화할 의무 없음
- 배포를 위한 독점 제품에 통합
- 우선 기술 지원 및 업데이트

**상업용 라이선스 문의:**
- 📧 이메일: contact@quantmesh.io
- 🌐 웹사이트: https://quantmesh.io/commercial

---

### 라이선스 세부 정보

이 프로젝트는 다음 듀얼 라이선스 하에 있습니다:

1. **AGPL-3.0 (GNU Affero General Public License v3.0)**
   - 사용, 수정 및 배포 무료
   - 모든 파생 작품은 AGPL-3.0 하에 오픈 소스화되어야 합니다
   - 네트워크 서비스의 경우에도 모든 사용자에게 소스 코드를 제공해야 합니다
   - 수정 사항은 커뮤니티에 기여되어야 합니다

2. **상업용 라이선스**
   - 독점 사용에 필요
   - 수정 사항을 오픈 소스화할 의무 없음
   - 우선 지원 및 업데이트 포함

상업용 라이선싱 문의는 다음으로 연락하세요:
- 📧 이메일: contact@quantmesh.io
- 🌐 웹사이트: https://quantmesh.io/commercial

## 🤝 기여

기여를 환영합니다! 다음과 같이 도울 수 있습니다:

- ⭐ **이 저장소에 별표 표시** 도움이 되었다면
- 🍴 **포크하고 사용** 프로젝트
- 🐛 **버그 보고** [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)를 통해
- 💡 **기능 제안** [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)를 통해
- 📝 **개선을 위한 PR 제출**
- 📖 **문서 개선**

**참고:** AGPL-3.0 라이선스에 따라 이 프로젝트에 대한 모든 기여는 동일한 AGPL-3.0 라이선스 하에 릴리스됩니다.

자세한 지침은 [CONTRIBUTING.md](../CONTRIBUTING.md)를 참조하세요.

## 🙏 감사의 말

원본 프로젝트 [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker)의 [dennisyang1986](https://github.com/dennisyang1986)에게 이 프로젝트의 견고한 기반을 제공한 오픈 소스 기여에 감사드립니다. 자세한 내용은 [NOTICE](../../NOTICE) 파일을 참조하세요.

---

## 📞 연락처 및 지원

- 🌐 **웹사이트**: https://quantmesh.io
- 📧 **이메일**: contact@quantmesh.io
- 💬 **Discord**: [커뮤니티에 가입](https://discord.gg/YOUR_INVITE_LINK)
- 🐛 **Issues**: [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💬 **토론**: [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📖 **문서**: [전체 문서](../)

---

<div align="center">
  <strong>QuantMesh Team이 ❤️로 제작</strong><br/>
  <sub>이 프로젝트가 도움이 되었다면 ⭐를 주는 것을 고려해 주세요</sub>
</div>

Copyright © 2025 QuantMesh Team. All Rights Reserved.

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
