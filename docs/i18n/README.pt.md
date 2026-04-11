<div align="center">
  <img src="../../assets/logo.svg" alt="QuantMesh Logo" width="600"/>
  
  # QuantMesh Market Maker
  
  **Criador de Mercado de Criptomoedas de Alta Frequência**

  [![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue.svg)](https://golang.org/dl/)
  [![License](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](../../LICENSE)
  
  [简体中文](../../README.md) | [繁體中文](README.zh-TW.md) | [English](README.en.md) | [Español](README.es.md) | [Français](README.fr.md) | [Português](README.pt.md)
</div>

---

## 📖 Introdução

QuantMesh é um sistema de criador de mercado de criptomoedas de alto desempenho e baixa latência, focado em estratégias de trading de grid unidirecional para mercados de contratos perpétuos. Desenvolvido em Go e alimentado por fluxos de dados em tempo real via WebSocket, visa fornecer suporte de liquidez estável para principais exchanges como Binance, Bitget e Gate.io.

Após várias iterações, usamos este sistema para negociar mais de $100 milhões em criptomoedas. Por exemplo, negociando ETHUSDC da Binance com zero taxas, um intervalo de preço de $1 e $300 por ordem, o volume de negociação diário pode exceder $3 milhões, e mais de $50 milhões por mês. Enquanto o mercado estiver oscilando ou tendendo para cima, continuará gerando lucros. Se o mercado cair unilateralmente, $30.000 em margem podem garantir que não haja liquidação por uma queda de 1000 pontos. Através de negociação contínua para reduzir custos, uma recuperação de 50% é suficiente para atingir o ponto de equilíbrio, e retornar ao preço de abertura original pode gerar lucros substanciais. Se houver uma queda rápida unilateral, o sistema de controle de risco ativo identificará automaticamente e imediatamente interromperá a negociação, permitindo ordens contínuas apenas quando o mercado se recuperar, sem se preocupar com liquidação por picos de preço.

Exemplo: Começando a negociar ETH a 3000 pontos, o preço cai para 2700 pontos, perdendo aproximadamente $3.000. Quando o preço se recupera para mais de 2850 pontos, atinge o ponto de equilíbrio. Voltando para 3000 pontos, os lucros variam entre $1.000 e $3.000.

## 📜 Origem do Projeto

Este projeto foi desenvolvido originalmente com base em [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker), publicado por [dennisyang1986](https://github.com/dennisyang1986) sob a Licença MIT.

Com base no projeto original, fizemos as seguintes melhorias e extensões principais:

- ✨ **Interface Frontend Completa**: Adicionada uma interface de gerenciamento web React + TypeScript fornecendo monitoramento de negociação visual, gerenciamento de configuração e análise de dados
- 🏦 **Expansão de Exchanges**: Expandido de 3 exchanges (Binance, Bitget, Gate.io) no projeto original para **20+ exchanges principais**
- 🔒 **Estabilidade de Nível Financeiro**: Melhorada abrangentemente a confiabilidade do sistema, incluindo tratamento completo de erros, mecanismos de segurança de concorrência, garantias de consistência de dados, recuperação automática, etc.
- 📊 **Monitoramento Aprimorado**: Sistema de registro aprimorado, coleta de métricas (Prometheus), verificações de saúde e alertas em tempo real
- 🛡️ **Controle de Risco Reforçado**: Monitoramento de risco multicamadas, reconciliação automática, disjuntor de anomalias e proteção de segurança de fundos
- 🔌 **Sistema de Plugins**: Suporte para mecanismos de plugins extensíveis para personalização fácil e desenvolvimento secundário
- 📱 **Suporte de Internacionalização**: Interface multilíngue (Chinês/Inglês), suporte i18n
- 🧪 **Suporte Testnet**: Suporte para ambientes testnet de múltiplas exchanges para desenvolvimento e testes

Para descrições detalhadas de melhorias e informações de software de terceiros, consulte o arquivo [NOTICE](../../NOTICE).

**Nota Importante**: Este projeto agora é distribuído sob a **GNU Affero General Public License v3.0 (AGPL-3.0)**. De acordo com os requisitos da Licença MIT do projeto original, mantivemos o reconhecimento do projeto original.

## ✨ Características Principais

- **Suporte Multi-Exchange**: Compatível com Binance, Bitget, Gate.io, Bybit, EdgeX e outras plataformas principais.
- **Resposta em Nível de Milissegundo**: Totalmente alimentado por WebSocket (dados de mercado e fluxo de ordens), eliminando atrasos de polling.
- **Estratégia de Grid Inteligente**: 
  - **Modo de Quantidade Fixa**: Utilização de capital mais controlável.
  - **Sistema Super Slot**: Gerencia inteligentemente os estados de ordens e posições, prevenindo conflitos de concorrência.
- **Sistema Poderoso de Controle de Risco**:
  - **Controle de Risco Ativo**: Monitoramento em tempo real de anomalias de volume de K-line, pausando automaticamente a negociação.
  - **Segurança de Fundos**: Verifica automaticamente o saldo, alavancagem e risco máximo de posição antes da inicialização.
  - **Reconciliação Automática**: Sincroniza regularmente os estados locais e da exchange para garantir consistência de dados.
- **Arquitetura de Alta Concorrência**: Modelo de concorrência eficiente baseado em Goroutine + Channel + Sync.Map.

## 🏦 Exchanges Suportadas

| Exchange | Status | Volume de Negociação Diário | Notas |
|----------|--------|----------------------------|-------|
| **Binance** | ✅ Stable | $50B+ | Maior exchange do mundo |
| **Bitget** | ✅ Stable | $10B+ | Plataforma principal de negociação de futuros |
| **Gate.io** | ✅ Stable | $5B+ | Exchange estabelecida |
| **OKX** | ✅ Stable | $20B+ | Top 3 globalmente, forte base de usuários chineses |
| **Bybit** | ✅ Stable | $15B+ | Plataforma principal de negociação de futuros |
| **Huobi (HTX)** | ✅ Stable | $5B+ | Exchange estabelecida, mercado chinês forte |
| **KuCoin** | ✅ Stable | $3B+ | Altcoins ricos, suporte a contratos de futuros |
| **Kraken** | ✅ Stable | $2B+ | Forte conformidade, principal na Europa e América |
| **Bitfinex** | ✅ Stable | $1B+ | Exchange estabelecida, boa liquidez |
| **MEXC** | ✅ Stable | $8B+ | Grande volume de negociação de futuros, altcoins ricos, testnet suportado |
| **BingX** | ✅ Stable | $3B+ | Plataforma de negociação social, boa experiência de futuros, testnet suportado |
| **Deribit** | ✅ Stable | $2B+ | Maior exchange de opções do mundo, suporta futuros + opções, testnet suportado |
| **BitMEX** | ✅ Stable | $2B+ | Exchange de derivativos estabelecida, até 100x alavancagem, testnet suportado |
| **Phemex** | ✅ Stable | $2B+ | Negociação de futuros sem taxas, motor de alto desempenho, testnet suportado |
| **WOO X** | ✅ Stable | $1.5B+ | Exchange de nível institucional, liquidez profunda, testnet suportado |
| **CoinEx** | ✅ Stable | $1B+ | Exchange estabelecida (2017), altcoins ricos, testnet suportado |
| **Bitrue** | ✅ Stable | $1B+ | Exchange principal do ecossistema XRP, mercado do Sudeste Asiático forte, testnet suportado |
| **XT.COM** | ✅ Stable | $800M+ | Exchange emergente, altcoins ricos, testnet suportado |
| **BTCC** | ✅ Stable | $500M+ | Exchange estabelecida (2011), primeira exchange Bitcoin da China, testnet suportado |
| **AscendEX** | ✅ Stable | $400M+ | Exchange de nível institucional, amigável ao DeFi, testnet suportado |
| **Poloniex** | ✅ Stable | $300M+ | Exchange estabelecida (2014), rica variedade de moedas, testnet suportado |
| **Crypto.com** | ✅ Stable | $500M+ | Marca conhecida, dezenas de milhões de usuários globalmente, testnet suportado |

## Arquitetura de Módulos

```
quantmesh_platform/
├── main.go                    # Ponto de entrada do programa principal, orquestração de componentes
│
├── config/                    # Gerenciamento de configuração
│   └── config.go              # Carregamento e validação de configuração YAML
│
├── exchange/                  # Camada de abstração de exchange (núcleo)
│   ├── interface.go           # Interface unificada IExchange
│   ├── factory.go             # Padrão de fábrica para criar instâncias de exchange
│   ├── types.go               # Estruturas de dados comuns
│   ├── wrapper_*.go           # Adaptadores (envolvendo exchanges)
│   ├── binance/               # Implementação da Binance
│   ├── bitget/                # Implementação do Bitget
│   └── gate/                  # Implementação do Gate.io
│
├── logger/                    # Sistema de registro
│   └── logger.go              # Registro de arquivo + registro de console
│
├── monitor/                   # Monitoramento de preços
│   └── price_monitor.go       # Fluxo de preços único global
│
├── order/                     # Camada de execução de ordens
│   └── executor_adapter.go    # Executor de ordens (limitação de taxa + nova tentativa)
│
├── position/                  # Gerenciamento de posições (núcleo)
│   └── super_position_manager.go  # Gerenciador de slots super
│
├── safety/                    # Segurança e controle de risco
│   ├── safety.go              # Verificações de segurança pré-inicialização
│   ├── risk_monitor.go        # Controle de risco ativo (monitoramento de K-line)
│   ├── reconciler.go          # Reconciliação de posições
│   └── order_cleaner.go       # Limpeza de ordens
│
└── utils/                     # Funções utilitárias
    └── orderid.go             # Geração de ID de ordem personalizado
```

## Melhores Práticas

1. **Para Status VIP de Exchange**: Este sistema é uma ferramenta de geração de volume. Se as flutuações de preço não forem grandes, $3.000 em margem podem gerar $10 milhões em volume de negociação em 2-3 dias.

2. **Melhor Prática para Lucros**: Entre no mercado após uma rodada de queda. Primeiro compre uma posição, depois inicie o software. Ele venderá automaticamente grid por grid para cima. Quando sua posição estiver esgotada, pare o sistema. Se não tiver certeza se o mercado atual é um ponto baixo, pode começar sem uma posição base. Se cair mais, adicione uma posição no ponto baixo e reinicie para continuar vendendo. Isso maximiza os lucros. Repita este ciclo para lucros contínuos. Não se preocupe com quedas: o programa reduz continuamente os custos. Desde que se recupere pela metade, você atinge o ponto de equilíbrio.

## 🚀 Início Rápido

### Pré-requisitos
- Go 1.21 ou superior
- Ambiente de rede capaz de acessar APIs de exchange

### Instalação

1. **Clonar o repositório**
   ```bash
   git clone https://github.com/ghostsworm/quantmesh.git
   cd quantmesh
   ```

2. **Instalar dependências**
   ```bash
   go mod download
   ```

### Configuração

> **Runtime SSOT:** The primary database table `app_config` holds the authoritative configuration. One-time YAML import: `./quantmesh --migrate-app-config` (with `QUANTMESH_IMPORT_YAML`, or `config.yaml` in the working directory), or run `./quantmesh /path/to/file.yaml` as the first argument. See [`docs/config-database-design.md`](../config-database-design.md).

1. Copie o arquivo de configuração de exemplo:
   ```bash
   cp docs/config/examples/config.example.yaml config.yaml
   ```

2. Edite `config.yaml` e preencha sua API Key e parâmetros de estratégia:

   ```yaml
   app:
     current_exchange: "binance"  # Selecionar exchange

   exchanges:
     binance:
       api_key: "YOUR_API_KEY"
       secret_key: "YOUR_SECRET_KEY"
       fee_rate: 0.0002

   trading:
     symbol: "ETHUSDT"       # Par de negociação
     price_interval: 2       # Espaçamento de grid (preço)
     order_quantity: 30     # Quantidade por grid (USDT)
     buy_window_size: 10    # Número de ordens de compra
     sell_window_size: 10   # Número de ordens de venda
   ```

### Uso

```bash
go run main.go
```

Ou compile e execute:

```bash
go build -o quantmesh
./quantmesh
```

## 🏗️ Arquitetura

O sistema adota um design modular com componentes principais incluindo:

- **Camada de Exchange**: Abstração de interface de exchange unificada, protegendo diferenças de API subjacentes.
- **Monitor de Preços**: Fonte de preços WebSocket única global, garantindo consistência de decisões.
- **Gerenciador de Posição Super**: Gerenciador de posições principal, gerenciando o ciclo de vida de ordens baseado no mecanismo Slot.
- **Segurança e Controle de Risco**: Controle de risco multicamadas, incluindo verificações de inicialização, monitoramento em tempo de execução e disjuntor de anomalias.

Para documentação de arquitetura mais detalhada, consulte [ARCHITECTURE.md](../../ARCHITECTURE.md).

## ⚠️ Aviso Legal

Este software é apenas para fins educacionais e de pesquisa. A negociação de criptomoedas envolve alto risco e pode resultar em perda de capital.
- Os usuários são os únicos responsáveis por quaisquer lucros ou perdas resultantes do uso deste software.
- Sempre teste minuciosamente no Testnet antes de usar fundos reais.
- Os desenvolvedores não são responsáveis por perdas devido a bugs de software, latência de rede ou falhas de exchange.

## 📜 Licença

Este projeto usa um **modelo de Licença Dupla**:

### Licença de Código Aberto AGPL-3.0
- ✅ Livre para usar, modificar e distribuir
- ⚠️ **Todas as obras derivadas devem ser de código aberto** e publicadas sob AGPL-3.0
- ⚠️ O código-fonte deve ser fornecido mesmo para serviços de rede
- ⚠️ O código modificado deve ser devolvido à comunidade

### Licença Comercial
Se você precisar usar este software em aplicativos ou serviços proprietários, ou não desejar tornar suas modificações de código aberto, você precisa comprar uma licença comercial.

**Escopo da Licença Comercial:**
- Uso em aplicativos proprietários
- Sem obrigação de tornar as modificações de código aberto
- Integrar em produtos proprietários para distribuição
- Suporte técnico prioritário e atualizações

**Consultas de Licença Comercial:**
- 📧 Email: contact@quantmesh.io
- 🌐 Website: https://quantmesh.io/commercial

---

### Detalhes da Licença

Este projeto está sob licença dupla:

1. **AGPL-3.0 (GNU Affero General Public License v3.0)**
   - Livre para usar, modificar e distribuir
   - Todas as obras derivadas devem ser de código aberto sob AGPL-3.0
   - O código-fonte deve ser fornecido a todos os usuários, mesmo para serviços de rede
   - As modificações devem ser devolvidas à comunidade

2. **Licença Comercial**
   - Necessária para uso proprietário
   - Sem obrigação de tornar as modificações de código aberto
   - Inclui suporte prioritário e atualizações

Para consultas de licenciamento comercial, entre em contato:
- 📧 Email: contact@quantmesh.io
- 🌐 Website: https://quantmesh.io/commercial

## 🤝 Contribuindo

Bem-vindo para enviar Issues e Pull Requests!

**Nota:** De acordo com a licença AGPL-3.0, todas as contribuições para este projeto serão publicadas sob a mesma licença AGPL-3.0.

## 🙏 Agradecimentos

Obrigado ao projeto original [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker) por [dennisyang1986](https://github.com/dennisyang1986) por sua contribuição de código aberto, que forneceu uma base sólida para este projeto. Para mais informações, consulte o arquivo [NOTICE](../../NOTICE).

---
Copyright © 2025 QuantMesh Team. All Rights Reserved.

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
