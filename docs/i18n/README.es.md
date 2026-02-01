<div align="center">
  <img src="../assets/logo.svg" alt="QuantMesh Logo" width="600"/>
  
  # QuantMesh Market Maker
  
  **Creador de Mercado de Criptomonedas de Alta Frecuencia**

  [![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue.svg)](https://golang.org/dl/)
  [![License](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](../LICENSE)
  
  [繁體中文](../README.md) | [简体中文](README.zh-Hans.md) | [English](README.en.md) | [Español](README.es.md) | [Français](README.fr.md) | [Português](README.pt.md)
</div>

---

## 📖 Introducción

QuantMesh es un sistema de creador de mercado de criptomonedas de alto rendimiento y baja latencia que se enfoca en estrategias de trading de grid unidireccional para mercados de contratos perpetuos. Desarrollado en Go e impulsado por flujos de datos en tiempo real a través de WebSocket, tiene como objetivo proporcionar soporte de liquidez estable para intercambios principales como Binance, Bitget y Gate.io.

Después de varias iteraciones, hemos usado este sistema para operar más de $100 millones en criptomonedas. Por ejemplo, operando ETHUSDC de Binance con cero comisiones, un intervalo de precio de $1 y $300 por orden, el volumen de trading diario puede superar los $3 millones, y más de $50 millones por mes. Mientras el mercado esté oscilando o tendiendo al alza, continuará generando ganancias. Si el mercado cae unilateralmente, $30,000 en margen pueden garantizar que no haya liquidación por una caída de 1000 puntos. A través del trading continuo para reducir costos, una recuperación del 50% es suficiente para alcanzar el punto de equilibrio, y volver al precio de apertura original puede generar ganancias sustanciales. Si hay una caída rápida unilateral, el sistema de control de riesgo activo identificará automáticamente e inmediatamente detendrá el trading, solo permitiendo órdenes continuas cuando el mercado se recupere, sin preocuparse por la liquidación por picos de precio.

Ejemplo: Comenzando a operar ETH a 3000 puntos, el precio cae a 2700 puntos, perdiendo aproximadamente $3,000. Cuando el precio se recupera a más de 2850 puntos, alcanza el punto de equilibrio. Volviendo a 3000 puntos, las ganancias oscilan entre $1,000 y $3,000.

## 📜 Origen del Proyecto

Este proyecto se desarrolló originalmente basado en [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker), publicado por [dennisyang1986](https://github.com/dennisyang1986) bajo la Licencia MIT.

Basado en el proyecto original, hemos realizado las siguientes mejoras y extensiones principales:

- ✨ **Interfaz Frontend Completa**: Agregada una interfaz de gestión web React + TypeScript que proporciona monitoreo de trading visual, gestión de configuración y análisis de datos
- 🏦 **Expansión de Intercambios**: Expandido de 3 intercambios (Binance, Bitget, Gate.io) en el proyecto original a **20+ intercambios principales**
- 🔒 **Estabilidad de Grado Financiero**: Mejorada integralmente la confiabilidad del sistema, incluyendo manejo completo de errores, mecanismos de seguridad de concurrencia, garantías de consistencia de datos, recuperación automática, etc.
- 📊 **Monitoreo Mejorado**: Sistema de registro mejorado, recopilación de métricas (Prometheus), verificaciones de salud y alertas en tiempo real
- 🛡️ **Control de Riesgo Reforzado**: Monitoreo de riesgo multicapa, reconciliación automática, corte de circuito de anomalías y protección de seguridad de fondos
- 🔌 **Sistema de Plugins**: Soporte para mecanismos de plugins extensibles para personalización fácil y desarrollo secundario
- 📱 **Soporte de Internacionalización**: Interfaz multiidioma (Chino/Inglés), soporte i18n
- 🧪 **Soporte de Testnet**: Soporte para entornos de testnet de múltiples intercambios para desarrollo y pruebas

Para descripciones detalladas de mejoras e información de software de terceros, consulte el archivo [NOTICE](../NOTICE).

**Nota Importante**: Este proyecto ahora se distribuye bajo la **GNU Affero General Public License v3.0 (AGPL-3.0)**. De acuerdo con los requisitos de la Licencia MIT del proyecto original, hemos conservado el reconocimiento del proyecto original.

## ✨ Características Principales

- **Soporte Multi-Intercambio**: Compatible con Binance, Bitget, Gate.io, Bybit, EdgeX y otras plataformas principales.
- **Respuesta a Nivel de Milisegundos**: Totalmente impulsado por WebSocket (datos de mercado y flujo de órdenes), eliminando retrasos de sondeo.
- **Estrategia de Grid Inteligente**: 
  - **Modo de Cantidad Fija**: Utilización de capital más controlable.
  - **Sistema Super Slot**: Gestiona inteligentemente los estados de órdenes y posiciones, previniendo conflictos de concurrencia.
- **Sistema Poderoso de Control de Riesgo**:
  - **Control de Riesgo Activo**: Monitoreo en tiempo real de anomalías de volumen de K-line, pausando automáticamente el trading.
  - **Seguridad de Fondos**: Verifica automáticamente el balance, apalancamiento y riesgo máximo de posición antes del inicio.
  - **Reconciliación Automática**: Sincroniza regularmente los estados locales y del intercambio para garantizar la consistencia de datos.
- **Arquitectura de Alta Concurrencia**: Modelo de concurrencia eficiente basado en Goroutine + Channel + Sync.Map.

## 🏦 Intercambios Soportados

| Intercambio | Estado | Volumen de Trading Diario | Notas |
|-------------|--------|---------------------------|-------|
| **Binance** | ✅ Stable | $50B+ | Intercambio más grande del mundo |
| **Bitget** | ✅ Stable | $10B+ | Plataforma principal de trading de futuros |
| **Gate.io** | ✅ Stable | $5B+ | Intercambio establecido |
| **OKX** | ✅ Stable | $20B+ | Top 3 globalmente, fuerte base de usuarios chinos |
| **Bybit** | ✅ Stable | $15B+ | Plataforma principal de trading de futuros |
| **Huobi (HTX)** | ✅ Stable | $5B+ | Intercambio establecido, mercado chino fuerte |
| **KuCoin** | ✅ Stable | $3B+ | Altcoins ricos, soporte de contratos de futuros |
| **Kraken** | ✅ Stable | $2B+ | Fuerte cumplimiento, principal en Europa y América |
| **Bitfinex** | ✅ Stable | $1B+ | Intercambio establecido, buena liquidez |
| **MEXC** | ✅ Stable | $8B+ | Gran volumen de trading de futuros, altcoins ricos, testnet soportado |
| **BingX** | ✅ Stable | $3B+ | Plataforma de trading social, buena experiencia de futuros, testnet soportado |
| **Deribit** | ✅ Stable | $2B+ | Intercambio de opciones más grande del mundo, soporta futuros + opciones, testnet soportado |
| **BitMEX** | ✅ Stable | $2B+ | Intercambio de derivados establecido, hasta 100x apalancamiento, testnet soportado |
| **Phemex** | ✅ Stable | $2B+ | Trading de futuros sin comisiones, motor de alto rendimiento, testnet soportado |
| **WOO X** | ✅ Stable | $1.5B+ | Intercambio de grado institucional, liquidez profunda, testnet soportado |
| **CoinEx** | ✅ Stable | $1B+ | Intercambio establecido (2017), altcoins ricos, testnet soportado |
| **Bitrue** | ✅ Stable | $1B+ | Intercambio principal del ecosistema XRP, mercado del sudeste asiático fuerte, testnet soportado |
| **XT.COM** | ✅ Stable | $800M+ | Intercambio emergente, altcoins ricos, testnet soportado |
| **BTCC** | ✅ Stable | $500M+ | Intercambio establecido (2011), primer intercambio de Bitcoin de China, testnet soportado |
| **AscendEX** | ✅ Stable | $400M+ | Intercambio de grado institucional, amigable con DeFi, testnet soportado |
| **Poloniex** | ✅ Stable | $300M+ | Intercambio establecido (2014), rica variedad de monedas, testnet soportado |
| **Crypto.com** | ✅ Stable | $500M+ | Marca conocida, decenas de millones de usuarios globalmente, testnet soportado |

## Arquitectura de Módulos

```
quantmesh_platform/
├── main.go                    # Punto de entrada del programa principal, orquestación de componentes
│
├── config/                    # Gestión de configuración
│   └── config.go              # Carga y validación de configuración YAML
│
├── exchange/                  # Capa de abstracción de intercambio (núcleo)
│   ├── interface.go           # Interfaz unificada IExchange
│   ├── factory.go             # Patrón de fábrica para crear instancias de intercambio
│   ├── types.go               # Estructuras de datos comunes
│   ├── wrapper_*.go           # Adaptadores (envolviendo intercambios)
│   ├── binance/               # Implementación de Binance
│   ├── bitget/                # Implementación de Bitget
│   └── gate/                  # Implementación de Gate.io
│
├── logger/                    # Sistema de registro
│   └── logger.go              # Registro de archivos + registro de consola
│
├── monitor/                   # Monitoreo de precios
│   └── price_monitor.go       # Flujo de precios único global
│
├── order/                     # Capa de ejecución de órdenes
│   └── executor_adapter.go    # Ejecutor de órdenes (limitación de velocidad + reintento)
│
├── position/                  # Gestión de posiciones (núcleo)
│   └── super_position_manager.go  # Administrador de slots superiores
│
├── safety/                    # Seguridad y control de riesgo
│   ├── safety.go              # Verificaciones de seguridad previas al inicio
│   ├── risk_monitor.go        # Control de riesgo activo (monitoreo de K-line)
│   ├── reconciler.go          # Reconciliación de posiciones
│   └── order_cleaner.go        # Limpieza de órdenes
│
└── utils/                     # Funciones de utilidad
    └── orderid.go             # Generación de ID de orden personalizado
```

## Mejores Prácticas

1. **Para Estado VIP de Intercambio**: Este sistema es una herramienta de generación de volumen. Si las fluctuaciones de precio no son grandes, $3,000 en margen pueden generar $10 millones en volumen de trading en 2-3 días.

2. **Mejor Práctica para Ganancias**: Ingrese al mercado después de una ronda de caída. Primero compre una posición, luego inicie el software. Venderá automáticamente grid por grid hacia arriba. Cuando su posición se agote, detenga el sistema. Si no está seguro de si el mercado actual es un punto bajo, puede comenzar sin una posición base. Si cae más, agregue una posición en el punto bajo y reinicie para continuar vendiendo. Esto maximiza las ganancias. Repita este ciclo para obtener ganancias continuas. No se preocupe por las caídas: el programa reduce continuamente los costos. Mientras se recupere a la mitad, alcanzará el punto de equilibrio.

## 🚀 Inicio Rápido

### Prerrequisitos
- Go 1.21 o superior
- Entorno de red capaz de acceder a las APIs de intercambio

### Instalación

1. **Clonar el repositorio**
   ```bash
   git clone https://github.com/dennisyang1986/quantmesh_market_maker.git
   cd quantmesh_market_maker
   ```

2. **Instalar dependencias**
   ```bash
   go mod download
   ```

### Configuración

1. Copie el archivo de configuración de ejemplo:
   ```bash
   cp config.example.yaml config.yaml
   ```

2. Edite `config.yaml` y complete su API Key y parámetros de estrategia:

   ```yaml
   app:
     current_exchange: "binance"  # Seleccionar intercambio

   exchanges:
     binance:
       api_key: "YOUR_API_KEY"
       secret_key: "YOUR_SECRET_KEY"
       fee_rate: 0.0002

   trading:
     symbol: "ETHUSDT"       # Par de trading
     price_interval: 2       # Espaciado de grid (precio)
     order_quantity: 30     # Cantidad por grid (USDT)
     buy_window_size: 10    # Número de órdenes de compra
     sell_window_size: 10   # Número de órdenes de venta
   ```

### Uso

```bash
go run main.go
```

O compile y ejecute:

```bash
go build -o quantmesh
./quantmesh
```

## 🏗️ Arquitectura

El sistema adopta un diseño modular con componentes principales que incluyen:

- **Capa de Intercambio**: Abstracción de interfaz de intercambio unificada, protegiendo las diferencias de API subyacentes.
- **Monitor de Precios**: Fuente de precios WebSocket única global, garantizando consistencia de decisiones.
- **Administrador de Posición Super**: Administrador de posiciones principal, gestionando el ciclo de vida de órdenes basado en el mecanismo Slot.
- **Seguridad y Control de Riesgo**: Control de riesgo multicapa, incluyendo verificaciones de inicio, monitoreo en tiempo de ejecución y corte de circuito de anomalías.

Para documentación de arquitectura más detallada, consulte [ARCHITECTURE.md](../ARCHITECTURE.md).

## ⚠️ Descargo de Responsabilidad

Este software es solo para fines educativos y de investigación. El trading de criptomonedas implica alto riesgo y puede resultar en pérdida de capital.
- Los usuarios son los únicos responsables de cualquier ganancia o pérdida por el uso de este software.
- Siempre pruebe exhaustivamente en Testnet antes de usar fondos reales.
- Los desarrolladores no son responsables de pérdidas debido a errores de software, latencia de red o fallas del intercambio.

## 📜 Licencia

Este proyecto utiliza un **modelo de Licencia Dual**:

### Licencia de Código Abierto AGPL-3.0
- ✅ Libre de usar, modificar y distribuir
- ⚠️ **Todas las obras derivadas deben ser de código abierto** y publicadas bajo AGPL-3.0
- ⚠️ El código fuente debe proporcionarse incluso para servicios de red
- ⚠️ El código modificado debe contribuirse de vuelta a la comunidad

### Licencia Comercial
Si necesita usar este software en aplicaciones o servicios propietarios, o no desea hacer de código abierto sus modificaciones, necesita comprar una licencia comercial.

**Alcance de la Licencia Comercial:**
- Uso en aplicaciones propietarias
- Sin obligación de hacer de código abierto las modificaciones
- Integrar en productos propietarios para distribución
- Soporte técnico prioritario y actualizaciones

**Consultas de Licencia Comercial:**
- 📧 Email: contact@quantmesh.io
- 🌐 Website: https://quantmesh.io/commercial

---

### Detalles de la Licencia

Este proyecto tiene doble licencia bajo:

1. **AGPL-3.0 (GNU Affero General Public License v3.0)**
   - Libre de usar, modificar y distribuir
   - Todas las obras derivadas deben ser de código abierto bajo AGPL-3.0
   - El código fuente debe proporcionarse a todos los usuarios, incluso para servicios de red
   - Las modificaciones deben contribuirse de vuelta a la comunidad

2. **Licencia Comercial**
   - Requerida para uso propietario
   - Sin obligación de hacer de código abierto las modificaciones
   - Incluye soporte prioritario y actualizaciones

Para consultas de licencias comerciales, contacte:
- 📧 Email: contact@quantmesh.io
- 🌐 Website: https://quantmesh.io/commercial

## 🤝 Contribuir

¡Bienvenido a enviar Issues y Pull Requests!

**Nota:** De acuerdo con la licencia AGPL-3.0, todas las contribuciones a este proyecto se publicarán bajo la misma licencia AGPL-3.0.

## 🙏 Agradecimientos

Gracias al proyecto original [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker) por [dennisyang1986](https://github.com/dennisyang1986) por su contribución de código abierto, que proporcionó una base sólida para este proyecto. Para más información, consulte el archivo [NOTICE](../NOTICE).

---
Copyright © 2025 QuantMesh Team. All Rights Reserved.

