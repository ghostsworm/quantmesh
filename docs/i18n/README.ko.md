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

## 📊 성능 지표

- **거래량**: $100M+ 프로덕션 테스트됨
- **응답 지연 시간**: <10ms (WebSocket 구동)
- **지원 거래소**: 20+
- **동시 처리**: 1000+ 주문/초
- **시스템 가용성**: 99.9%+
- **일일 거래 용량**: $3M+ /일 (예: ETHUSDC)

---

## 📖 소개

QuantMesh는 영구 계약 시장을 위한 롱 그리드 거래 전략에 중점을 둔 고성능, 저지연 암호화폐 마켓 메이커 시스템입니다. Go로 개발되었으며 WebSocket 실시간 데이터 스트림으로 구동되며, Binance, Bitget 및 Gate.io와 같은 주요 거래소에 안정적인 유동성 지원을 제공하는 것을 목표로 합니다.

여러 반복을 거쳐 이 시스템을 사용하여 $1억 이상의 가상 통화를 거래했습니다. 예를 들어, 수수료 없이 Binance ETHUSDC를 거래하고, $1의 가격 간격과 주문당 $300으로 일일 거래량은 $300만을 초과할 수 있으며, 월 $5000만 이상입니다. 시장이 진동하거나 상승 추세에 있는 한 계속해서 수익을 창출합니다. 시장이 일방적으로 하락하는 경우 $30,000의 마진으로 1000포인트 하락에 대해 청산을 보장할 수 있습니다. 비용을 낮추기 위한 지속적인 거래를 통해 50% 회복으로 손익분기점에 도달하고 원래 시작 가격으로 돌아가면 상당한 수익을 얻을 수 있습니다. 일방적인 급락이 있는 경우 활성 위험 관리 시스템이 자동으로 식별하고 즉시 거래를 중지하며, 시장이 회복될 때만 지속적인 주문을 허용하여 가격 급등으로 인한 청산을 걱정할 필요가 없습니다.

예: 3000포인트에서 ETH 거래를 시작하고, 가격이 2700포인트로 하락하여 약 $3,000 손실. 가격이 2850포인트 이상으로 회복되면 손익분기점에 도달합니다. 3000포인트로 돌아가면 수익은 $1,000에서 $3,000 범위입니다.

## 📜 프로젝트 기원

이 프로젝트는 원래 MIT 라이선스 하에 [dennisyang1986](https://github.com/dennisyang1986)이 게시한 [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker)를 기반으로 개발되었습니다.

원본 프로젝트를 기반으로 다음과 같은 주요 개선 및 확장을 수행했습니다:

- ✨ **완전한 프론트엔드 인터페이스**: 시각적 거래 모니터링, 구성 관리 및 데이터 분석을 제공하는 React + TypeScript 웹 관리 인터페이스 추가
- 🏦 **거래소 확장**: 원본 프로젝트의 3개 거래소(Binance, Bitget, Gate.io)에서 **20+ 주요 거래소**로 확장
- 🔒 **금융 등급 안정성**: 포괄적인 오류 처리, 동시성 안전 메커니즘, 데이터 일관성 보장, 자동 복구 등을 포함하여 시스템 안정성을 포괄적으로 개선
- 📊 **향상된 모니터링**: 개선된 로깅 시스템, 메트릭 수집(Prometheus), 상태 확인 및 실시간 경고
- 🛡️ **강화된 위험 관리**: 다층 위험 모니터링, 자동 조정, 이상 회로 차단 및 자금 안전 보호
- 🔌 **플러그인 시스템**: 쉬운 사용자 정의 및 2차 개발을 위한 확장 가능한 플러그인 메커니즘 지원
- 📱 **국제화 지원**: 다국어 인터페이스(중국어/영어), i18n 지원
- 🧪 **테스트넷 지원**: 개발 및 테스트를 위한 여러 거래소의 테스트넷 환경 지원

자세한 개선 설명 및 타사 소프트웨어 정보는 [NOTICE](../../NOTICE) 파일을 참조하세요.

**중요 참고**: 이 프로젝트는 이제 **GNU Affero General Public License v3.0 (AGPL-3.0)** 하에 배포됩니다. 원본 프로젝트의 MIT 라이선스 요구 사항에 따라 원본 프로젝트의 인정을 유지했습니다.

## ✨ 주요 기능

- **다중 거래소 지원**: Binance, Bitget, Gate.io, Bybit, EdgeX 및 기타 주요 플랫폼과 호환됩니다.
- **밀리초 수준 응답**: 완전히 WebSocket 구동(시장 데이터 및 주문 흐름), 폴링 지연 제거.
- **스마트 그리드 전략**: 
  - **고정 금액 모드**: 더 제어 가능한 자본 활용.
  - **슈퍼 슬롯 시스템**: 주문 및 포지션 상태를 지능적으로 관리하여 동시성 충돌 방지.
- **강력한 위험 관리 시스템**:
  - **활성 위험 관리**: K선 볼륨 이상의 실시간 모니터링, 자동으로 거래 일시 중지.
  - **자금 안전**: 시작 전에 자동으로 잔액, 레버리지 및 최대 포지션 위험을 확인합니다.
  - **자동 조정**: 데이터 일관성을 보장하기 위해 로컬 및 거래소 상태를 정기적으로 동기화합니다.
- **높은 동시성 아키텍처**: Goroutine + Channel + Sync.Map를 기반으로 한 효율적인 동시성 모델.

## 🏦 지원 거래소

| 거래소 | 상태 | 일일 거래량 | 참고 |
|----------|--------|------------------------|-------|
| **Binance** | ✅ Stable | $50B+ | 세계 최대 거래소 |
| **Bitget** | ✅ Stable | $10B+ | 주류 선물 거래 플랫폼 |
| **Gate.io** | ✅ Stable | $5B+ | 확립된 거래소 |
| **OKX** | ✅ Stable | $20B+ | 전 세계 상위 3위, 강력한 중국 사용자 기반 |
| **Bybit** | ✅ Stable | $15B+ | 주류 선물 거래 플랫폼 |
| **Huobi (HTX)** | ✅ Stable | $5B+ | 확립된 거래소, 강력한 중국 시장 |
| **KuCoin** | ✅ Stable | $3B+ | 풍부한 알트코인, 선물 계약 지원 |
| **Kraken** | ✅ Stable | $2B+ | 강력한 규정 준수, 유럽 및 미국에서 주류 |
| **Bitfinex** | ✅ Stable | $1B+ | 확립된 거래소, 좋은 유동성 |
| **MEXC** | ✅ Stable | $8B+ | 큰 선물 거래량, 풍부한 알트코인, 테스트넷 지원 |
| **BingX** | ✅ Stable | $3B+ | 소셜 거래 플랫폼, 좋은 선물 경험, 테스트넷 지원 |
| **Deribit** | ✅ Stable | $2B+ | 세계 최대 옵션 거래소, 선물 + 옵션 지원, 테스트넷 지원 |
| **BitMEX** | ✅ Stable | $2B+ | 확립된 파생상품 거래소, 최대 100배 레버리지, 테스트넷 지원 |
| **Phemex** | ✅ Stable | $2B+ | 제로 수수료 선물 거래, 고성능 엔진, 테스트넷 지원 |
| **WOO X** | ✅ Stable | $1.5B+ | 기관급 거래소, 깊은 유동성, 테스트넷 지원 |
| **CoinEx** | ✅ Stable | $1B+ | 확립된 거래소(2017), 풍부한 알트코인, 테스트넷 지원 |
| **Bitrue** | ✅ Stable | $1B+ | 주요 XRP 생태계 거래소, 강력한 동남아시아 시장, 테스트넷 지원 |
| **XT.COM** | ✅ Stable | $800M+ | 신흥 거래소, 풍부한 알트코인, 테스트넷 지원 |
| **BTCC** | ✅ Stable | $500M+ | 확립된 거래소(2011), 중국 최초의 Bitcoin 거래소, 테스트넷 지원 |
| **AscendEX** | ✅ Stable | $400M+ | 기관급 거래소, DeFi 친화적, 테스트넷 지원 |
| **Poloniex** | ✅ Stable | $300M+ | 확립된 거래소(2014), 풍부한 코인 다양성, 테스트넷 지원 |
| **Crypto.com** | ✅ Stable | $500M+ | 잘 알려진 브랜드, 전 세계 수천만 사용자, 테스트넷 지원 |

## 모듈 아키텍처

```
quantmesh_platform/
├── main.go                    # 메인 프로그램 진입점, 구성 요소 오케스트레이션
│
├── config/                    # 구성 관리
│   └── config.go              # YAML 구성 로드 및 검증
│
├── exchange/                  # 거래소 추상화 계층(코어)
│   ├── interface.go           # IExchange 통합 인터페이스
│   ├── factory.go             # 거래소 인스턴스 생성을 위한 팩토리 패턴
│   ├── types.go               # 공통 데이터 구조
│   ├── wrapper_*.go           # 어댑터(거래소 래핑)
│   ├── binance/               # Binance 구현
│   ├── bitget/                # Bitget 구현
│   └── gate/                  # Gate.io 구현
│
├── logger/                    # 로깅 시스템
│   └── logger.go              # 파일 로깅 + 콘솔 로깅
│
├── monitor/                   # 가격 모니터링
│   └── price_monitor.go       # 전역 고유 가격 스트림
│
├── order/                     # 주문 실행 계층
│   └── executor_adapter.go    # 주문 실행자(속도 제한 + 재시도)
│
├── position/                  # 포지션 관리(코어)
│   └── super_position_manager.go  # 슈퍼 슬롯 관리자
│
├── safety/                    # 안전 및 위험 관리
│   ├── safety.go              # 시작 전 안전 검사
│   ├── risk_monitor.go        # 활성 위험 관리(K선 모니터링)
│   ├── reconciler.go          # 포지션 조정
│   └── order_cleaner.go       # 주문 정리
│
└── utils/                     # 유틸리티 함수
    └── orderid.go             # 사용자 정의 주문 ID 생성
```

## 모범 사례

1. **거래소 VIP 상태**: 이 시스템은 볼륨 생성 도구입니다. 가격 변동이 크지 않으면 $3,000의 마진으로 2-3일 내에 $1000만의 거래량을 생성할 수 있습니다.

2. **수익을 위한 모범 사례**: 하락 라운드 후 시장에 진입합니다. 먼저 포지션을 구매한 다음 소프트웨어를 시작합니다. 자동으로 그리드별로 위로 판매합니다. 포지션이 매진되면 시스템을 중지합니다. 현재 시장이 저점인지 확실하지 않은 경우 기본 포지션 없이 시작할 수 있습니다. 더 하락하면 저점에서 포지션을 추가하고 재시작하여 판매를 계속합니다. 이것은 수익을 극대화합니다. 이 사이클을 반복하여 지속적으로 수익을 얻습니다. 하락에 대해 걱정하지 마세요 - 프로그램은 지속적으로 비용을 낮춥니다. 절반만 회복되면 손익분기점에 도달합니다.

## 🚀 시작하기

### 전제 조건
- Go 1.21 이상
- 거래소 API에 액세스할 수 있는 네트워크 환경

### 설치

1. **저장소 복제**
   ```bash
   git clone https://github.com/ghostsworm/quantmesh.git
   cd quantmesh
   ```

2. **의존성 설치**
   ```bash
   go mod download
   ```

### 구성

> **Runtime SSOT:** The primary database table `app_config` holds the authoritative configuration. One-time YAML import: `./quantmesh --migrate-app-config` (with `QUANTMESH_IMPORT_YAML`, or `config.yaml` in the working directory), or run `./quantmesh /path/to/file.yaml` as the first argument. See [`docs/config-database-design.md`](../config-database-design.md).

1. 예제 구성 파일 복사:
   ```bash
   cp docs/config/examples/config.example.yaml config.yaml
   ```

2. `config.yaml`을 편집하고 API 키 및 전략 매개변수를 입력:

   ```yaml
   app:
     current_exchange: "binance"  # 거래소 선택

   exchanges:
     binance:
       api_key: "YOUR_API_KEY"
       secret_key: "YOUR_SECRET_KEY"
       fee_rate: 0.0002

   trading:
     symbol: "ETHUSDT"       # 거래 쌍
     price_interval: 2       # 그리드 간격(가격)
     order_quantity: 30     # 그리드당 금액(USDT)
     buy_window_size: 10    # 매수 주문 수
     sell_window_size: 10   # 매도 주문 수
   ```

### 사용법

#### 프로덕션 모드

컴파일된 바이너리 실행:

```bash
go run main.go
```

또는 빌드하고 실행:

```bash
go build -o quantmesh
./quantmesh
```

백엔드는 포트 28888(기본값)에서 프론트엔드 정적 파일을 제공합니다.

#### 개발 모드

핫 리로드 및 소스 코드 디버깅을 위한 프론트엔드 개발:

**옵션 1: 개발 스크립트 사용(권장)**

```bash
./scripts/local/dev.sh
```

이 스크립트는:
- 포트 28888에서 Go 백엔드 서버 시작
- 포트 15173에서 Vite dev 서버 시작
- 프론트엔드 코드 변경에 대한 핫 리로드 활성화
- 디버깅을 위한 소스 맵 제공(최소화되지 않은 코드)

그런 다음 애플리케이션에 액세스: **http://localhost:15173**

**옵션 2: 수동 시작**

터미널 1 - Go 백엔드 시작:
```bash
go run main.go
```

터미널 2 - Vite dev 서버 시작:
```bash
cd webui
pnpm dev
```

그런 다음 애플리케이션에 액세스: **http://localhost:15173**

**개발 모드 이점:**
- ✅ 핫 리로드 - 프론트엔드 코드 변경이 즉시 반영됨
- ✅ 소스 맵 - 원본 TypeScript/React 코드로 디버그(최소화되지 않음)
- ✅ 빠른 새로고침 - React 구성 요소가 상태를 잃지 않고 업데이트됨
- ✅ 더 나은 오류 메시지 - 실제 파일 이름 및 줄 번호 보기

**참고:** 개발 모드에서 Vite dev 서버는 API 요청(`/api/*`) 및 WebSocket 연결(`/ws`)을 포트 28888에서 실행 중인 Go 백엔드로 프록시합니다.

## 🏗️ 아키텍처

시스템은 다음을 포함한 핵심 구성 요소로 모듈식 설계를 채택합니다:

- **거래소 계층**: 통합 거래소 인터페이스 추상화, 기본 API 차이 차폐.
- **가격 모니터**: 전역 고유 WebSocket 가격 소스, 의사 결정 일관성 보장.
- **슈퍼 포지션 관리자**: 핵심 포지션 관리자, Slot 메커니즘을 기반으로 주문 수명 주기 관리.
- **안전 및 위험 관리**: 다층 위험 관리, 시작 검사, 런타임 모니터링 및 이상 회로 차단 포함.

더 자세한 아키텍처 문서는 [ARCHITECTURE.md](../ARCHITECTURE.md)를 참조하세요.

## 📊 사용 통계 및 개인정보 보호

QuantMesh는 익명 사용 데이터를 수집하는 선택적 사용 통계 기능을 포함하여 프로젝트 사용을 이해하고 제품을 개선하는 데 도움이 됩니다. **모든 데이터 수집은 완전히 투명하며, 코드는 감사 가능하고 언제든지 비활성화할 수 있습니다.**

### 🔒 개인정보 보호

**수집하는 데이터(익명):**
- ✅ **기본 정보**: 버전 번호, 운영 체제, 아키텍처, 인스턴스 ID(무작위로 생성된 UUID)
- ✅ **사용 통계**: 사용된 거래소 이름, 거래 쌍
- ✅ **성능 메트릭**: API 요청/응답 지연 시간, WebSocket 지연 시간
- ✅ **거래 활동**: 거래 방향(매수/매도), 거래 금액 제외

**수집하지 않는 데이터:**
- ❌ **IP 주소**: 프론트엔드는 IP 캡처가 비활성화되어 있으며, 백엔드는 IP 대신 인스턴스 ID를 사용합니다
- ❌ **지리적 위치**: 위도/경도, 도시 또는 기타 위치 정보 수집 없음
- ❌ **개인 정보**: 사용자 ID, 이메일, 이름 또는 기타 신원 정보 수집 없음
- ❌ **민감한 데이터**: API 키, 거래 금액, 계정 잔액 또는 포지션 정보 수집 없음
- ❌ **금융 데이터**: 금융 또는 거래 민감한 정보 수집 없음

### 🛡️ 개인정보 보호 조치

1. **인스턴스 ID 메커니즘**: 고유 식별자로 무작위로 생성된 UUID를 사용하며, `./data/instance_id` 파일에 저장되며 개인 정보를 포함하지 않습니다
2. **프론트엔드 IP 비활성화**: PostHog SDK가 `ip_capture: false`로 구성되어 IP 주소 캡처 및 지리적 위치 추론을 비활성화합니다
3. **백엔드는 IP를 전송하지 않음**: 백엔드 코드는 통계 서비스에 IP 주소를 전송하지 않습니다
4. **완전히 선택 사항**: 사용자는 환경 변수를 통해 언제든지 통계를 비활성화할 수 있습니다
5. **코드 투명성**: 모든 통계 코드는 감사 가능하며 `utils/telemetry.go`에 있습니다

### ⚙️ 통계를 비활성화하는 방법

**방법 1: 환경 변수(권장)**
```bash
export QUANTMESH_DISABLE_TELEMETRY=1
```

**방법 2: 프론트엔드 비활성화**
`webui/.env.local` 파일에서:
```bash
VITE_DISABLE_TELEMETRY=1
```

**방법 3: 코드 수정**
`utils/telemetry.go`를 편집하고 `Enabled`를 `false`로 설정

### 📖 자세한 문서

통계 기능에 대한 자세한 정보는 다음을 참조하세요:
- 📖 [완전한 통계 가이드](../../docs/TELEMETRY_GUIDE.md)
- 🔒 [개인정보 보호 가이드](../../docs/TELEMETRY_PRIVACY.md)
- 🚀 [빠른 설정 가이드](../../docs/TELEMETRY_SIMPLE_GUIDE.md)

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
