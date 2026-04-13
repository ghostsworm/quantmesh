<div align="center">
  <img src="../../logo/qm_thick_tail_white.svg" alt="QuantMesh Logo" width="200"/>
  
  # QuantMesh Market Maker
  
  **Высокочастотный криптовалютный маркет-мейкер**

  [![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue.svg)](https://golang.org/dl/)
  [![License](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](../../LICENSE)
  [![GitHub Stars](https://img.shields.io/github/stars/ghostsworm/quantmesh.svg?style=social&label=Stars)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Forks](https://img.shields.io/github/forks/ghostsworm/quantmesh.svg?style=social&label=Forks)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Issues](https://img.shields.io/github/issues/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/issues)
  [![GitHub Release](https://img.shields.io/github/release/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/releases)
  [![Website](https://img.shields.io/badge/Website-quantmesh.io-green.svg)](https://quantmesh.io)
  
  [简体中文](../../README.md) | [繁體中文](README.zh-TW.md) | [English](README.en.md) | [Español](README.es.md) | [Français](README.fr.md) | [Português](README.pt.md) | [Русский](README.ru.md)
</div>

---

## 🎯 Почему выбрать QuantMesh?

| Функция | QuantMesh | Другие решения |
|---------|-----------|----------------|
| **Поддержка бирж** | 20+ бирж | Обычно 3-5 |
| **Задержка отклика** | Уровень миллисекунд | Уровень секунд |
| **Управление рисками** | Многоуровневый активный контроль | Базовый контроль |
| **Протестировано в продакшене** | $100M+ объем торгов | Не протестировано |
| **Веб-интерфейс** | ✅ Полный React UI | ❌ Нет/Базовый |
| **Открытый исходный код** | AGPL-3.0 | Закрытый исходный код/Ограниченный |
| **Данные в реальном времени** | Только WebSocket | REST polling |
| **Параллелизм** | 1000+ ордеров/сек | Ограниченный |

**Ключевые преимущества:**
- ✅ **Проверено в бою**: Доказано объемом торгов $100M+
- ✅ **Высокая производительность**: Задержка менее 10 мс с архитектурой WebSocket
- ✅ **Комплексное**: Полное решение от торговли до мониторинга
- ✅ **Прозрачное**: Полностью открытый исходный код, проверяемый код
- ✅ **Расширяемое**: Система плагинов для настройки

---

## 📊 Telemetry

**Third-party analytics (PostHog) has been removed.** The Go backend and install script no longer send usage events. Symbols in `utils/telemetry.go` are no-ops for compatibility.

The Web UI may still load a **1×1 pixel** on startup (`webui/src/services/telemetry.ts`). Disable with `VITE_DISABLE_TELEMETRY=1` in `webui/.env.local` or `localStorage` key `QUANTMESH_DISABLE_TELEMETRY` = `1`.

See [TELEMETRY_GUIDE.md](../../docs/TELEMETRY_GUIDE.md).
---

## ⚠️ Отказ от ответственности

Это программное обеспечение предназначено только для образовательных и исследовательских целей. Торговля криптовалютами сопряжена с высоким риском и может привести к потере капитала.
- Пользователи несут единоличную ответственность за любую прибыль или убытки от использования этого программного обеспечения.
- Всегда тщательно тестируйте на тестовой сети перед использованием реальных средств.
- Разработчики не несут ответственности за убытки из-за ошибок программного обеспечения, сетевой задержки или сбоев биржи.

## 🪙 Поддержка криптоплатежей

QuantMesh поддерживает криптовалютные платежи для подписок и лицензий:

### Поддерживаемые криптовалюты
- **BTC** (Bitcoin)
- **ETH** (Ethereum)
- **USDT** (Tether, ERC20)
- **USDC** (USD Coin, ERC20)

### Способы оплаты
1. **Coinbase Commerce** (Рекомендуется)
   - Автоматическое подтверждение
   - Поддержка нескольких криптовалют
   - Простая страница оплаты

2. **Прямой платеж кошелька**
   - Без участия третьих лиц
   - Больше конфиденциальности
   - Ручное подтверждение (1-24 часа)

### Быстрый старт
```bash
# Метод A: Coinbase Commerce (15 минут)
# 1. Зарегистрироваться на https://commerce.coinbase.com
# 2. Настроить API-ключ в .env.crypto
# 3. Запустить службу

# Метод B: Прямой кошелек (5 минут)
# 1. Настроить адреса кошелька
# 2. Запустить службу
# 3. Ручное подтверждение
```

### Документация
- 📖 [Руководство по платежам пользователя](../CRYPTO_PAYMENT_GUIDE.md)
- 🚀 [Руководство по быстрому старту](../CRYPTO_PAYMENT_QUICKSTART.md)
- 🔧 [Руководство по настройке](../CRYPTO_PAYMENT_SETUP.md)
- 📊 [Сводка реализации](../reports/CRYPTO_PAYMENT_SUMMARY.md)

### Почему криптоплатежи?
✅ Не требуется кредитная карта или банковский счет  
✅ Глобальная доступность, без региональных ограничений  
✅ Более низкие комиссии за транзакции (1% vs 2.9%)  
✅ Лучшая защита конфиденциальности  
✅ Быстрое подтверждение (10-30 минут)  
✅ Идеально подходит для программного обеспечения для торговли криптовалютами  

## 📜 Лицензия

Этот проект использует **модель двойной лицензии**:

### Лицензия открытого исходного кода AGPL-3.0
- ✅ Бесплатно использовать, изменять и распространять
- ⚠️ **Все производные работы должны быть открыты** и выпущены под AGPL-3.0
- ⚠️ Исходный код должен быть предоставлен даже для сетевых служб
- ⚠️ Измененный код должен быть возвращен сообществу

### Коммерческая лицензия
Если вам нужно использовать это программное обеспечение в проприетарных приложениях или службах, или вы не хотите открывать исходный код ваших изменений, вам нужно приобрести коммерческую лицензию.

**Область коммерческой лицензии:**
- Использование в проприетарных приложениях
- Нет обязательства открывать исходный код изменений
- Интеграция в проприетарные продукты для распространения
- Приоритетная техническая поддержка и обновления

**Запросы коммерческой лицензии:**
- 📧 Email: contact@quantmesh.io
- 🌐 Веб-сайт: https://quantmesh.io/commercial

---

### Детали лицензии

Этот проект имеет двойную лицензию под:

1. **AGPL-3.0 (GNU Affero General Public License v3.0)**
   - Бесплатно для использования, изменения и распространения
   - Все производные работы должны быть открыты под AGPL-3.0
   - Исходный код должен быть предоставлен всем пользователям, даже для сетевых служб
   - Изменения должны быть возвращены сообществу

2. **Коммерческая лицензия**
   - Требуется для проприетарного использования
   - Нет обязательства открывать исходный код изменений
   - Включает приоритетную поддержку и обновления

Для запросов коммерческого лицензирования обращайтесь:
- 📧 Email: contact@quantmesh.io
- 🌐 Веб-сайт: https://quantmesh.io/commercial

## 🤝 Вклад

Мы приветствуем вклад! Вот как вы можете помочь:

- ⭐ **Поставить звезду этому репозиторию**, если вы находите его полезным
- 🍴 **Форкнуть и использовать** проект
- 🐛 **Сообщить об ошибках** через [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💡 **Предложить функции** через [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📝 **Отправить PR** для улучшений
- 📖 **Улучшить документацию**

**Примечание:** Согласно лицензии AGPL-3.0, все вклады в этот проект будут выпущены под той же лицензией AGPL-3.0.

См. [CONTRIBUTING.md](../CONTRIBUTING.md) для подробных рекомендаций.

## 🙏 Благодарности

Спасибо оригинальному проекту [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker) от [dennisyang1986](https://github.com/dennisyang1986) за их вклад в открытый исходный код, который обеспечил прочную основу для этого проекта. Для получения дополнительной информации см. файл [NOTICE](../../NOTICE).

---

## 📞 Контакты и поддержка

- 🌐 **Веб-сайт**: https://quantmesh.io
- 📧 **Email**: contact@quantmesh.io
- 💬 **Discord**: [Присоединиться к нашему сообществу](https://discord.gg/YOUR_INVITE_LINK)
- 🐛 **Issues**: [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💬 **Обсуждения**: [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📖 **Документация**: [Полная документация](../)

---

<div align="center">
  <strong>Сделано с ❤️ командой QuantMesh</strong><br/>
  <sub>Если вы находите этот проект полезным, пожалуйста, рассмотрите возможность поставить ему ⭐</sub>
</div>

Copyright © 2025 QuantMesh Team. Все права защищены.

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
