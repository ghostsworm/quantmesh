<div align="center">
  <img src="../../logo/qm_thick_tail_white.svg" alt="QuantMesh Logo" width="200"/>
  
  # QuantMesh Market Maker
  
  **Високочастотний Маркет-Мейкер Криптовалют**

  [![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue.svg)](https://golang.org/dl/)
  [![License](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](../../LICENSE)
  [![GitHub Stars](https://img.shields.io/github/stars/ghostsworm/quantmesh.svg?style=social&label=Stars)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Forks](https://img.shields.io/github/forks/ghostsworm/quantmesh.svg?style=social&label=Forks)](https://github.com/ghostsworm/quantmesh)
  [![GitHub Issues](https://img.shields.io/github/issues/ghostsworm/quantmesh.svg)](https://github.com/ghostsworm/quantmesh/issues)
  [![GitHub Release](https://img.shields.io/badge/Release-GitHub-blue.svg)](https://github.com/ghostsworm/quantmesh/releases)
  [![Website](https://img.shields.io/badge/Website-quantmesh.io-green.svg)](https://quantmesh.io)
  
  [简体中文](../../README.md) | [繁體中文](README.zh-TW.md) | [English](README.en.md) | [Español](README.es.md) | [Français](README.fr.md) | [Português](README.pt.md) | [Українська](README.uk.md)
</div>

---

## 🎯 Чому обирати QuantMesh?

| Функція | QuantMesh | Інші рішення |
|---------|-----------|----------------|
| **Підтримка бірж** | 20+ бірж | Зазвичай 3-5 |
| **Затримка відповіді** | Рівень мілісекунд | Рівень секунд |
| **Контроль ризиків** | Багатошаровий активний контроль | Базовий контроль |
| **Протестовано в продакшені** | Торговий об'єм $100M+ | Не протестовано |
| **Веб-інтерфейс** | ✅ Повний React UI | ❌ Відсутній/Базовий |
| **Відкритий код** | AGPL-3.0 | Закритий код/Обмежений |
| **Дані в реальному часі** | Тільки WebSocket | REST polling |
| **Паралельність** | 1000+ ордерів/сек | Обмежена |

**Ключові переваги:**
- ✅ **Перевірено в бою**: Підтверджено торговим об'ємом $100M+
- ✅ **Висока продуктивність**: Затримка менше 10ms з архітектурою WebSocket
- ✅ **Комплексне**: Повне рішення від торгівлі до моніторингу
- ✅ **Прозоре**: Повністю відкритий код, код підлягає аудиту
- ✅ **Розширюване**: Система плагінів для налаштування

---

## 📊 Telemetry

**Third-party analytics (PostHog) has been removed.** The Go backend and install script no longer send usage events. Symbols in `utils/telemetry.go` are no-ops for compatibility.

The Web UI may still load a **1×1 pixel** on startup (`webui/src/services/telemetry.ts`). Disable with `VITE_DISABLE_TELEMETRY=1` in `webui/.env.local` or `localStorage` key `QUANTMESH_DISABLE_TELEMETRY` = `1`.

See [TELEMETRY_GUIDE.md](../../docs/TELEMETRY_GUIDE.md).
---

## ⚠️ Відмова від відповідальності

Це програмне забезпечення призначене лише для освітніх та дослідницьких цілей. Торгівля криптовалютами пов'язана з високим ризиком і може призвести до втрати капіталу.
- Користувачі несуть повну відповідальність за будь-які прибутки або збитки від використання цього програмного забезпечення.
- Завжди ретельно тестуйте на Testnet перед використанням реальних коштів.
- Розробники не несуть відповідальності за збитки через помилки програмного забезпечення, затримки мережі або збої бірж.

## 🪙 Підтримка криптоплатежів

QuantMesh підтримує криптовалютні платежі для підписок та ліцензій:

### Підтримувані криптовалюти
- **BTC** (Bitcoin)
- **ETH** (Ethereum)
- **USDT** (Tether, ERC20)
- **USDC** (USD Coin, ERC20)

### Методи оплати
1. **Coinbase Commerce** (Рекомендовано)
   - Автоматичне підтвердження
   - Підтримка кількох криптовалют
   - Легка сторінка оплати

2. **Прямий платіж гаманця**
   - Без участі третіх сторін
   - Більше конфіденційності
   - Ручне підтвердження (1-24 години)

### Швидкий старт
```bash
# Метод A: Coinbase Commerce (15 хвилин)
# 1. Зареєструйтеся на https://commerce.coinbase.com
# 2. Налаштуйте API Key у .env.crypto
# 3. Запустіть службу

# Метод B: Прямий гаманець (5 хвилин)
# 1. Налаштуйте адреси гаманця
# 2. Запустіть службу
# 3. Ручне підтвердження
```

### Документація
- 📖 [Посібник користувача з оплати](../CRYPTO_PAYMENT_GUIDE.md)
- 🚀 [Посібник зі швидкого старту](../CRYPTO_PAYMENT_QUICKSTART.md)
- 🔧 [Посібник з налаштування](../CRYPTO_PAYMENT_SETUP.md)
- 📊 [Підсумок реалізації](../reports/CRYPTO_PAYMENT_SUMMARY.md)

### Чому криптоплатежі?
✅ Не потрібна кредитна картка або банківський рахунок  
✅ Глобальна доступність, без регіональних обмежень  
✅ Нижчі комісії за транзакції (1% проти 2.9%)  
✅ Кращий захист конфіденційності  
✅ Швидке підтвердження (10-30 хвилин)  
✅ Ідеальна відповідність для програмного забезпечення торгівлі криптовалютами  

## 📜 Ліцензія

Цей проект використовує **модель подвійної ліцензії**:

### Ліцензія з відкритим кодом AGPL-3.0
- ✅ Безкоштовне використання, модифікація та розповсюдження
- ⚠️ **Всі похідні роботи повинні бути з відкритим кодом** та випущені під AGPL-3.0
- ⚠️ Вихідний код повинен бути наданий навіть для мережевих служб
- ⚠️ Модифікований код повинен бути повернений спільноті

### Комерційна ліцензія
Якщо вам потрібно використовувати це програмне забезпечення в пропрієтарних додатках або службах, або ви не хочете зробити свої модифікації відкритим кодом, вам потрібно придбати комерційну ліцензію.

**Обсяг комерційної ліцензії:**
- Використання в пропрієтарних додатках
- Немає зобов'язання робити модифікації відкритим кодом
- Інтеграція в пропрієтарні продукти для розповсюдження
- Пріоритетна технічна підтримка та оновлення

**Запити щодо комерційної ліцензії:**
- 📧 Email: contact@quantmesh.io
- 🌐 Веб-сайт: https://quantmesh.io/commercial

---

### Деталі ліцензії

Цей проект має подвійну ліцензію під:

1. **AGPL-3.0 (GNU Affero General Public License v3.0)**
   - Безкоштовно для використання, модифікації та розповсюдження
   - Всі похідні роботи повинні бути з відкритим кодом під AGPL-3.0
   - Вихідний код повинен бути наданий всім користувачам, навіть для мережевих служб
   - Модифікації повинні бути повернені спільноті

2. **Комерційна ліцензія**
   - Потрібна для пропрієтарного використання
   - Немає зобов'язання робити модифікації відкритим кодом
   - Включає пріоритетну підтримку та оновлення

Для запитів щодо комерційного ліцензування зв'яжіться:
- 📧 Email: contact@quantmesh.io
- 🌐 Веб-сайт: https://quantmesh.io/commercial

## 🤝 Внесок

Ми вітаємо внески! Ось як ви можете допомогти:

- ⭐ **Поставте зірку цьому репозиторію**, якщо вважаєте його корисним
- 🍴 **Сфоркуйте та використовуйте** проект
- 🐛 **Повідомте про помилки** через [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💡 **Запропонуйте функції** через [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📝 **Надішліть PR** для покращень
- 📖 **Покращте документацію**

**Примітка:** Відповідно до ліцензії AGPL-3.0, всі внески до цього проекту будуть випущені під тією ж ліцензією AGPL-3.0.

Див. [CONTRIBUTING.md](../CONTRIBUTING.md) для детальних рекомендацій.

## 🙏 Подяки

Дякуємо оригінальному проекту [OpenSQT Market Maker](https://github.com/dennisyang1986/opensqt_market_maker) від [dennisyang1986](https://github.com/dennisyang1986) за їхній внесок з відкритим кодом, який забезпечив міцну основу для цього проекту. Для більшої інформації див. файл [NOTICE](../../NOTICE).

---

## 📞 Контакти та підтримка

- 🌐 **Веб-сайт**: https://quantmesh.io
- 📧 **Email**: contact@quantmesh.io
- 💬 **Discord**: [Приєднайтеся до нашої спільноти](https://discord.gg/YOUR_INVITE_LINK)
- 🐛 **Issues**: [GitHub Issues](https://github.com/ghostsworm/quantmesh/issues)
- 💬 **Обговорення**: [GitHub Discussions](https://github.com/ghostsworm/quantmesh/discussions)
- 📖 **Документація**: [Повна документація](../)

---

<div align="center">
  <strong>Зроблено з ❤️ командою QuantMesh</strong><br/>
  <sub>Якщо ви вважаєте цей проект корисним, будь ласка, розгляньте можливість поставити йому ⭐</sub>
</div>

Copyright © 2025 QuantMesh Team. Всі права захищені.

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
