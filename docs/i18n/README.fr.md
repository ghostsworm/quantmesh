<div align="center">
  <img src="../assets/logo.svg" alt="QuantMesh Logo" width="600"/>
  
  # QuantMesh Market Maker
  
  **Créateur de Marché Crypto à Haute Fréquence**

  [![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue.svg)](https://golang.org/dl/)
  [![License](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](LICENSE)
  
  [English](../README.md) | [中文](README.zh.md) | [Español](README.es.md) | [Français](README.fr.md) | [Português](README.pt.md)
</div>

---

## 📖 Introduction

QuantMesh est un système de créateur de marché de cryptomonnaies haute performance et à faible latence, axé sur les stratégies de trading en grille unidirectionnelle pour les marchés de contrats perpétuels. Développé en Go et alimenté par des flux de données en temps réel via WebSocket, il vise à fournir un support de liquidité stable pour les principales bourses comme Binance, Bitget et Gate.io.

Après plusieurs itérations, nous avons utilisé ce système pour trader plus de 100 millions de dollars en cryptomonnaies. Par exemple, en tradant ETHUSDC de Binance avec zéro frais, un intervalle de prix de 1 $ et 300 $ par ordre, le volume de trading quotidien peut dépasser 3 millions de dollars, et plus de 50 millions de dollars par mois. Tant que le marché oscille ou tend à la hausse, il continuera à générer des profits. Si le marché chute unilatéralement, 30 000 $ de marge peuvent garantir qu'il n'y ait pas de liquidation pour une baisse de 1000 points. Grâce au trading continu pour réduire les coûts, une reprise de 50 % suffit pour atteindre le seuil de rentabilité, et revenir au prix d'ouverture d'origine peut générer des profits substantiels. S'il y a une chute rapide unilatérale, le système de contrôle des risques actif identifiera automatiquement et arrêtera immédiatement le trading, n'autorisant les ordres continus que lorsque le marché se rétablit, sans se soucier de la liquidation par des pics de prix.

Exemple : Commencer à trader ETH à 3000 points, le prix chute à 2700 points, perdant environ 3 000 $. Lorsque le prix se rétablit à plus de 2850 points, il atteint le seuil de rentabilité. En revenant à 3000 points, les profits varient entre 1 000 $ et 3 000 $.

## 📜 Origine du Projet

Ce projet a été développé à l'origine sur la base de [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker), publié par [dennisyang1986](https://github.com/dennisyang1986) sous la licence MIT.

Basé sur le projet original, nous avons apporté les améliorations et extensions principales suivantes :

- ✨ **Interface Frontend Complète** : Ajout d'une interface de gestion web React + TypeScript fournissant une surveillance de trading visuelle, une gestion de configuration et une analyse de données
- 🏦 **Expansion des Bourses** : Étendu de 3 bourses (Binance, Bitget, Gate.io) dans le projet original à **20+ bourses principales**
- 🔒 **Stabilité de Niveau Financier** : Amélioration globale de la fiabilité du système, incluant une gestion complète des erreurs, des mécanismes de sécurité de concurrence, des garanties de cohérence des données, une récupération automatique, etc.
- 📊 **Surveillance Améliorée** : Système de journalisation amélioré, collecte de métriques (Prometheus), vérifications de santé et alertes en temps réel
- 🛡️ **Contrôle des Risques Renforcé** : Surveillance des risques multicouches, réconciliation automatique, disjoncteur d'anomalies et protection de la sécurité des fonds
- 🔌 **Système de Plugins** : Support pour des mécanismes de plugins extensibles pour une personnalisation facile et un développement secondaire
- 📱 **Support d'Internationalisation** : Interface multilingue (Chinois/Anglais), support i18n
- 🧪 **Support Testnet** : Support pour les environnements testnet de plusieurs bourses pour le développement et les tests

Pour des descriptions détaillées des améliorations et des informations sur les logiciels tiers, veuillez consulter le fichier [NOTICE](../NOTICE).

**Note Importante** : Ce projet est maintenant distribué sous la **GNU Affero General Public License v3.0 (AGPL-3.0)**. Conformément aux exigences de la licence MIT du projet original, nous avons conservé la reconnaissance du projet original.

## ✨ Caractéristiques Principales

- **Support Multi-Bourses** : Compatible avec Binance, Bitget, Gate.io, Bybit, EdgeX et d'autres plateformes principales.
- **Réponse au Niveau de la Milliseconde** : Entièrement alimenté par WebSocket (données de marché et flux d'ordres), éliminant les délais de sondage.
- **Stratégie de Grille Intelligente** : 
  - **Mode Montant Fixe** : Utilisation du capital plus contrôlable.
  - **Système Super Slot** : Gère intelligemment les états des ordres et des positions, prévenant les conflits de concurrence.
- **Système Puissant de Contrôle des Risques** :
  - **Contrôle des Risques Actif** : Surveillance en temps réel des anomalies de volume K-line, mettant automatiquement en pause le trading.
  - **Sécurité des Fonds** : Vérifie automatiquement le solde, l'effet de levier et le risque de position maximum avant le démarrage.
  - **Réconciliation Automatique** : Synchronise régulièrement les états locaux et de la bourse pour assurer la cohérence des données.
- **Architecture Haute Concurrence** : Modèle de concurrence efficace basé sur Goroutine + Channel + Sync.Map.

## 🏦 Bourses Supportées

| Bourse | Statut | Volume de Trading Quotidien | Notes |
|--------|--------|----------------------------|-------|
| **Binance** | ✅ Stable | $50B+ | Plus grande bourse au monde |
| **Bitget** | ✅ Stable | $10B+ | Plateforme principale de trading de contrats à terme |
| **Gate.io** | ✅ Stable | $5B+ | Bourse établie |
| **OKX** | ✅ Stable | $20B+ | Top 3 mondial, forte base d'utilisateurs chinois |
| **Bybit** | ✅ Stable | $15B+ | Plateforme principale de trading de contrats à terme |
| **Huobi (HTX)** | ✅ Stable | $5B+ | Bourse établie, marché chinois fort |
| **KuCoin** | ✅ Stable | $3B+ | Altcoins riches, support de contrats à terme |
| **Kraken** | ✅ Stable | $2B+ | Forte conformité, principal en Europe et en Amérique |
| **Bitfinex** | ✅ Stable | $1B+ | Bourse établie, bonne liquidité |
| **MEXC** | ✅ Stable | $8B+ | Grand volume de trading de contrats à terme, altcoins riches, testnet supporté |
| **BingX** | ✅ Stable | $3B+ | Plateforme de trading social, bonne expérience de contrats à terme, testnet supporté |
| **Deribit** | ✅ Stable | $2B+ | Plus grande bourse d'options au monde, supporte contrats à terme + options, testnet supporté |
| **BitMEX** | ✅ Stable | $2B+ | Bourse de dérivés établie, jusqu'à 100x effet de levier, testnet supporté |
| **Phemex** | ✅ Stable | $2B+ | Trading de contrats à terme sans frais, moteur haute performance, testnet supporté |
| **WOO X** | ✅ Stable | $1.5B+ | Bourse de niveau institutionnel, liquidité profonde, testnet supporté |
| **CoinEx** | ✅ Stable | $1B+ | Bourse établie (2017), altcoins riches, testnet supporté |
| **Bitrue** | ✅ Stable | $1B+ | Bourse principale de l'écosystème XRP, marché de l'Asie du Sud-Est fort, testnet supporté |
| **XT.COM** | ✅ Stable | $800M+ | Bourse émergente, altcoins riches, testnet supporté |
| **BTCC** | ✅ Stable | $500M+ | Bourse établie (2011), première bourse Bitcoin de Chine, testnet supporté |
| **AscendEX** | ✅ Stable | $400M+ | Bourse de niveau institutionnel, favorable à DeFi, testnet supporté |
| **Poloniex** | ✅ Stable | $300M+ | Bourse établie (2014), riche variété de pièces, testnet supporté |
| **Crypto.com** | ✅ Stable | $500M+ | Marque connue, dizaines de millions d'utilisateurs dans le monde, testnet supporté |

## Architecture des Modules

```
quantmesh_platform/
├── main.go                    # Point d'entrée du programme principal, orchestration des composants
│
├── config/                    # Gestion de la configuration
│   └── config.go              # Chargement et validation de la configuration YAML
│
├── exchange/                  # Couche d'abstraction de bourse (noyau)
│   ├── interface.go           # Interface unifiée IExchange
│   ├── factory.go             # Modèle de fabrique pour créer des instances de bourse
│   ├── types.go               # Structures de données communes
│   ├── wrapper_*.go           # Adaptateurs (enveloppant les bourses)
│   ├── binance/               # Implémentation de Binance
│   ├── bitget/                # Implémentation de Bitget
│   └── gate/                  # Implémentation de Gate.io
│
├── logger/                    # Système de journalisation
│   └── logger.go              # Journalisation de fichiers + journalisation de console
│
├── monitor/                   # Surveillance des prix
│   └── price_monitor.go       # Flux de prix unique global
│
├── order/                     # Couche d'exécution des ordres
│   └── executor_adapter.go    # Exécuteur d'ordres (limitation de débit + nouvelle tentative)
│
├── position/                  # Gestion des positions (noyau)
│   └── super_position_manager.go  # Gestionnaire de slots super
│
├── safety/                    # Sécurité et contrôle des risques
│   ├── safety.go              # Vérifications de sécurité avant démarrage
│   ├── risk_monitor.go        # Contrôle des risques actif (surveillance K-line)
│   ├── reconciler.go          # Réconciliation des positions
│   └── order_cleaner.go        # Nettoyage des ordres
│
└── utils/                     # Fonctions utilitaires
    └── orderid.go             # Génération d'ID d'ordre personnalisé
```

## Meilleures Pratiques

1. **Pour le Statut VIP de Bourse** : Ce système est un outil de génération de volume. Si les fluctuations de prix ne sont pas importantes, 3 000 $ de marge peuvent générer 10 millions de dollars de volume de trading en 2-3 jours.

2. **Meilleure Pratique pour les Profits** : Entrez sur le marché après une série de baisse. Achetez d'abord une position, puis démarrez le logiciel. Il vendra automatiquement grille par grille vers le haut. Lorsque votre position est épuisée, arrêtez le système. Si vous n'êtes pas sûr que le marché actuel soit un point bas, vous pouvez commencer sans position de base. S'il baisse davantage, ajoutez une position au point bas et redémarrez pour continuer à vendre. Cela maximise les profits. Répétez ce cycle pour des profits continus. Ne vous inquiétez pas des baisses : le programme réduit continuellement les coûts. Tant qu'il se rétablit de moitié, vous atteignez le seuil de rentabilité.

## 🚀 Démarrage Rapide

### Prérequis
- Go 1.21 ou supérieur
- Environnement réseau capable d'accéder aux API de bourse

### Installation

1. **Cloner le dépôt**
   ```bash
   git clone https://github.com/dennisyang1986/quantmesh_market_maker.git
   cd quantmesh_market_maker
   ```

2. **Installer les dépendances**
   ```bash
   go mod download
   ```

### Configuration

1. Copiez le fichier de configuration d'exemple :
   ```bash
   cp config.example.yaml config.yaml
   ```

2. Éditez `config.yaml` et remplissez votre clé API et les paramètres de stratégie :

   ```yaml
   app:
     current_exchange: "binance"  # Sélectionner la bourse

   exchanges:
     binance:
       api_key: "YOUR_API_KEY"
       secret_key: "YOUR_SECRET_KEY"
       fee_rate: 0.0002

   trading:
     symbol: "ETHUSDT"       # Paire de trading
     price_interval: 2       # Espacement de grille (prix)
     order_quantity: 30     # Montant par grille (USDT)
     buy_window_size: 10    # Nombre d'ordres d'achat
     sell_window_size: 10   # Nombre d'ordres de vente
   ```

### Utilisation

```bash
go run main.go
```

Ou compilez et exécutez :

```bash
go build -o quantmesh
./quantmesh
```

## 🏗️ Architecture

Le système adopte une conception modulaire avec des composants principaux incluant :

- **Couche de Bourse** : Abstraction d'interface de bourse unifiée, masquant les différences d'API sous-jacentes.
- **Moniteur de Prix** : Source de prix WebSocket unique globale, assurant la cohérence des décisions.
- **Gestionnaire de Position Super** : Gestionnaire de positions principal, gérant le cycle de vie des ordres basé sur le mécanisme Slot.
- **Sécurité et Contrôle des Risques** : Contrôle des risques multicouches, incluant les vérifications de démarrage, la surveillance en temps d'exécution et le disjoncteur d'anomalies.

Pour une documentation d'architecture plus détaillée, veuillez consulter [ARCHITECTURE.md](../ARCHITECTURE.md).

## ⚠️ Avertissement

Ce logiciel est uniquement à des fins éducatives et de recherche. Le trading de cryptomonnaies implique un risque élevé et peut entraîner une perte de capital.
- Les utilisateurs sont les seuls responsables de tout profit ou perte résultant de l'utilisation de ce logiciel.
- Testez toujours minutieusement sur Testnet avant d'utiliser des fonds réels.
- Les développeurs ne sont pas responsables des pertes dues à des bugs logiciels, à la latence du réseau ou aux défaillances de la bourse.

## 📜 Licence

Ce projet utilise un **modèle de Licence Double** :

### Licence Open Source AGPL-3.0
- ✅ Libre d'utilisation, de modification et de distribution
- ⚠️ **Toutes les œuvres dérivées doivent être open source** et publiées sous AGPL-3.0
- ⚠️ Le code source doit être fourni même pour les services réseau
- ⚠️ Le code modifié doit être rendu à la communauté

### Licence Commerciale
Si vous devez utiliser ce logiciel dans des applications ou services propriétaires, ou ne souhaitez pas rendre open source vos modifications, vous devez acheter une licence commerciale.

**Portée de la Licence Commerciale :**
- Utilisation dans des applications propriétaires
- Aucune obligation de rendre open source les modifications
- Intégrer dans des produits propriétaires pour la distribution
- Support technique prioritaire et mises à jour

**Demandes de Licence Commerciale :**
- 📧 Email: contact@quantmesh.io
- 🌐 Website: https://quantmesh.io/commercial

---

### Détails de la Licence

Ce projet est sous double licence :

1. **AGPL-3.0 (GNU Affero General Public License v3.0)**
   - Libre d'utilisation, de modification et de distribution
   - Toutes les œuvres dérivées doivent être open source sous AGPL-3.0
   - Le code source doit être fourni à tous les utilisateurs, même pour les services réseau
   - Les modifications doivent être rendues à la communauté

2. **Licence Commerciale**
   - Requise pour un usage propriétaire
   - Aucune obligation de rendre open source les modifications
   - Inclut le support prioritaire et les mises à jour

Pour les demandes de licence commerciale, contactez :
- 📧 Email: contact@quantmesh.io
- 🌐 Website: https://quantmesh.io/commercial

## 🤝 Contribution

Bienvenue pour soumettre des Issues et des Pull Requests !

**Note :** Conformément à la licence AGPL-3.0, toutes les contributions à ce projet seront publiées sous la même licence AGPL-3.0.

## 🙏 Remerciements

Merci au projet original [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker) par [dennisyang1986](https://github.com/dennisyang1986) pour leur contribution open source, qui a fourni une base solide pour ce projet. Pour plus d'informations, veuillez consulter le fichier [NOTICE](../NOTICE).

---
Copyright © 2025 QuantMesh Team. All Rights Reserved.

