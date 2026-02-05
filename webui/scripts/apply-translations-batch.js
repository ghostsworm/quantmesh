/**
 * Apply translations for uk-UA, ur-PK, bn-BD, th-TH
 * Core UI strings - no external API
 */
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const LOCALES = path.join(__dirname, '../src/i18n/locales');

const uk = {
  sidebar: { newbieRiskCheck: "Перевірка ризиків для новачків", aiConfig: "AI-налаштування", aiTasks: "AI-завдання", strategyMarket: "Ринок стратегій", capitalManagement: "Управління капіталом", profitManagement: "Управління прибутком", positionPlan: "План позицій", backtest: "Бектест", tradingPanel: "Панель торгівлі", currentPositions: "Поточні позиції", orderManagement: "Управління ордерами", strategySlots: "Слоти стратегій", strategyAllocation: "Розподіл стратегій", profitStatistics: "Статистика прибутку", reconciliation: "Узгодження", riskMonitor: "Монітор ризиків", klineDepth: "Глибина K-лінії", fundingRate: "Ставка фінансування", basisMonitor: "Монітор базису", servicesStatus: "Статус сервісів", configManagement: "Конфігурація", firstTimeWizard: "Майстер першого запуску", profile: "Профіль", optimizer: "Оптимізатор параметрів", newsAnalysis: "Аналіз новин", dataExport: "Експорт даних", klineFiles: "Файли K-ліній", expandSidebar: "Розгорнути бічну панель", collapseSidebar: "Згорнути бічну панель", groupMonitor: "Моніторинг та огляд", groupBacktestData: "Бектест та дані", groupAI: "AI", groupStrategyCapital: "Стратегія та капітал" },
  backtest: { dataSource: "Джерело даних", dataSourceTimeRange: "Часовий діапазон", dataSourceKlineFile: "Файл K-лінії", dataSourceCache: "Кеш бектесту", selectKlineFile: "Вибрати файл K-лінії", selectCache: "Вибрати кеш", hasDepth: "Є глибина", depthRiskEnabled: "Контроль ризику глибини увімкнено", refreshFileList: "Оновити список файлів", refreshCacheList: "Оновити список кешу", cacheOptionalHint: "Можна запустити бектест напряму; якщо кешу немає, дані будуть автоматично завантажені з біржі та закешовані." },
  servicesStatus: { title: "Статус сервісів", hint: "Перевірте доступність зберігання, бектесту та інших backend-сервісів.", ok: "OK", unavailable: "Недоступно", normal: "Нормально", checkHint: "Перевірте конфігурацію та перезапустіть" },
  statusBar: { globalView: "Глобальний огляд", fundingRate: "Ставка фінансування", fundingRateError: "Не вдалося отримати ставку фінансування" },
  app: { logoutError: "Помилка виходу" },
  login: { title: "Вхід", password: "Пароль", passwordPlaceholder: "Введіть пароль", passwordLogin: "Вхід за паролем", loading: "Вхід...", webauthnLogin: "Біометричний вхід", verifying: "Перевірка...", webauthnNotRegistered: "Біометрія не зареєстрована", enterPassword: "Введіть пароль", passwordError: "Невірний пароль", webauthnLoginFailed: "Біометричний вхід не вдався", webauthnLoginError: "Помилка WebAuthn" },
  connection: { offlineTitle: "Інтернет відключено", offlineDesc: "Перевірте підключення до інтернету.", backendDisconnectedTitle: "Немає з'єднання з сервером", backendDisconnectedDesc: "Переконайтеся, що backend працює (localhost:28888)." },
  pwa: { newVersionDetected: "Є нова версія, оновіть сторінку", updateNow: "Оновити зараз?", canInstall: "PWA можна встановити", installed: "PWA встановлено" },
  dashboard: { tradingStopped: "Торгівля зупинена", tradingStarted: "Торгівля розпочата", operationFailed: "Операція не вдалася", status: "Статус", running: "Працює", stopped: "Зупинено", stop: "СТОП", start: "СТАРТ", totalPnL: "Загальний P&L", tradingVolume: "Обʼєм торгів" },
  configuration: { title: "Налаштування", saveChanges: "Зберегти зміни", loadFailed: "Помилка завантаження", saveSuccess: "Збережено успішно", saveFailed: "Помилка збереження" },
  orders: { title: "Управління ордерами", pendingTab: "В очікуванні", historyTab: "Історія", noPendingOrders: "Немає ордерів в очікуванні", buy: "Купити", sell: "Продати", cancelOrder: "Скасувати ордер", cancelAll: "Скасувати всі" }
};

const ur = {
  common: { logout: "لاگ آؤٹ", global: "عالمی", system: "سسٹم", trading: "ٹریڈنگ", version: "ورژن {{version}}", cancel: "منسوخ", close: "بند کریں", save: "محفوظ کریں", confirm: "تصدیق", loading: "لوڈ ہو رہا ہے...", retry: "دوبارہ کوشش کریں", refresh: "تازہ کریں", allExchanges: "تمام ایکسچینج", actions: "اقدامات", error: "خرابی" },
  sidebar: { overview: "جائزہ", performanceMonitor: "کارکردگی مانیٹر", eventCenter: "ایونٹ سینٹر", runLogs: "رن لاگز", aiPrompts: "AI پرامپٹس", newbieRiskCheck: "نئے صارف کے لیے رسک چیک", aiConfig: "AI کنفیگریشن", aiTasks: "AI ٹاسکس", strategyMarket: "حکمت عملی مارکیٹ", capitalManagement: "سرمایہ انتظام", profitManagement: "منافع انتظام", backtest: "بیک ٹیسٹ", tradingPanel: "ٹریڈنگ پینل", currentPositions: "موجودہ پوزیشنیں", orderManagement: "آرڈر انتظام", configManagement: "کنفیگریشن", firstTimeWizard: "پہلی بار سیٹ اپ ویزارڈ" },
  login: { title: "لاگ ان", password: "پاس ورڈ", passwordPlaceholder: "پاس ورڈ درج کریں", passwordLogin: "پاس ورڈ لاگ ان", loading: "لاگ ان ہو رہا ہے...", enterPassword: "پاس ورڈ درج کریں", passwordError: "غلط پاس ورڈ" },
  connection: { offlineTitle: "انٹرنیٹ منقطع", offlineDesc: "انٹرنیٹ کنکشن چیک کریں۔", backendDisconnectedTitle: "بیک اینڈ کنکشن نہیں", backendDisconnectedDesc: "بیک اینڈ چل رہا ہے یقینی بنائیں (localhost:28888)۔" },
  dashboard: { tradingStopped: "ٹریڈنگ بند", tradingStarted: "ٹریڈنگ شروع", status: "حیثیت", running: "چل رہا ہے", stopped: "بند", stop: "STOP", start: "START", totalPnL: "کل P&L" },
  configuration: { title: "ترتیبات", saveChanges: "تبدیلیاں محفوظ کریں", saveSuccess: "محفوظ ہو گیا", saveFailed: "محفوظ نہیں ہوا" },
  orders: { title: "آرڈر انتظام", pendingTab: "زیر التوا", historyTab: "تاریخ", noPendingOrders: "کوئی زیر التوا آرڈر نہیں", buy: "خریدیں", sell: "فروخت", cancelOrder: "آرڈر منسوخ", cancelAll: "سب منسوخ" }
};

const bn = {
  common: { logout: "লগ আউট", global: "গ্লোবাল", system: "সিস্টেম", trading: "ট্রেডিং", version: "সংস্করণ {{version}}", cancel: "বাতিল", close: "বন্ধ", save: "সংরক্ষণ", confirm: "নিশ্চিত", loading: "লোড হচ্ছে...", retry: "পুনরায় চেষ্টা", refresh: "রিফ্রেশ", allExchanges: "সমস্ত এক্সচেঞ্জ", actions: "ক্রিয়া", error: "ত্রুটি" },
  sidebar: { overview: "সংক্ষিপ্ত বিবরণ", performanceMonitor: "পারফরম্যান্স মনিটর", eventCenter: "ইভেন্ট সেন্টার", runLogs: "রান লগ", aiPrompts: "AI প্রম্পট", newbieRiskCheck: "নতুনদের ঝুঁকি পরীক্ষা", aiConfig: "AI কনফিগ", aiTasks: "AI টাস্ক", strategyMarket: "কৌশল বাজার", capitalManagement: "মূলধন ব্যবস্থাপনা", profitManagement: "লাভ ব্যবস্থাপনা", backtest: "ব্যাকটেস্ট", tradingPanel: "ট্রেডিং প্যানেল", currentPositions: "বর্তমান পজিশন", orderManagement: "অর্ডার ব্যবস্থাপনা", configManagement: "কনফিগারেশন", firstTimeWizard: "প্রথমবার সেটআপ উইজার্ড" },
  login: { title: "লগইন", password: "পাসওয়ার্ড", passwordPlaceholder: "পাসওয়ার্ড লিখুন", passwordLogin: "পাসওয়ার্ড লগইন", loading: "লগইন হচ্ছে...", enterPassword: "পাসওয়ার্ড লিখুন", passwordError: "ভুল পাসওয়ার্ড" },
  connection: { offlineTitle: "ইন্টারনেট সংযোগ বিচ্ছিন্ন", offlineDesc: "ইন্টারনেট সংযোগ পরীক্ষা করুন।", backendDisconnectedTitle: "ব্যাকএন্ড সংযোগ ব্যর্থ", backendDisconnectedDesc: "ব্যাকএন্ড চলছে কিনা নিশ্চিত করুন (localhost:28888)।" },
  dashboard: { tradingStopped: "ট্রেডিং বন্ধ", tradingStarted: "ট্রেডিং শুরু", status: "স্থিতি", running: "চলছে", stopped: "বন্ধ", stop: "STOP", start: "START", totalPnL: "মোট P&L" },
  configuration: { title: "সেটিংস", saveChanges: "পরিবর্তন সংরক্ষণ করুন", saveSuccess: "সংরক্ষিত হয়েছে", saveFailed: "সংরক্ষণ ব্যর্থ" },
  orders: { title: "অর্ডার ব্যবস্থাপনা", pendingTab: "অপেক্ষমাণ", historyTab: "ইতিহাস", noPendingOrders: "কোন অপেক্ষমাণ অর্ডার নেই", buy: "কিনুন", sell: "বিক্রয়", cancelOrder: "অর্ডার বাতিল", cancelAll: "সব বাতিল" }
};

const th = {
  common: { logout: "ออกจากระบบ", global: "ทั่วโลก", system: "ระบบ", trading: "การเทรด", version: "เวอร์ชัน {{version}}", cancel: "ยกเลิก", close: "ปิด", save: "บันทึก", confirm: "ยืนยัน", loading: "กำลังโหลด...", retry: "ลองอีกครั้ง", refresh: "รีเฟรช", allExchanges: "ทุก exchange", actions: "การดำเนินการ", error: "ข้อผิดพลาด" },
  sidebar: { overview: "ภาพรวม", performanceMonitor: "มอนิเตอร์ประสิทธิภาพ", eventCenter: "ศูนย์เหตุการณ์", runLogs: "ล็อกการทำงาน", aiPrompts: "AI Prompts", newbieRiskCheck: "ตรวจสอบความเสี่ยงสำหรับมือใหม่", aiConfig: "การตั้งค่า AI", aiTasks: "งาน AI", strategyMarket: "ตลาดกลยุทธ์", capitalManagement: "การจัดการเงินทุน", profitManagement: "การจัดการกำไร", backtest: "แบ็กเทสต์", tradingPanel: "แผงเทรด", currentPositions: "ตำแหน่งปัจจุบัน", orderManagement: "การจัดการออเดอร์", configManagement: "การตั้งค่า", firstTimeWizard: "ตัวช่วยตั้งค่าครั้งแรก" },
  login: { title: "เข้าสู่ระบบ", password: "รหัสผ่าน", passwordPlaceholder: "กรอกรหัสผ่าน", passwordLogin: "เข้าสู่ระบบด้วยรหัสผ่าน", loading: "กำลังเข้าสู่ระบบ...", enterPassword: "กรอกรหัสผ่าน", passwordError: "รหัสผ่านไม่ถูกต้อง" },
  connection: { offlineTitle: "ตัดการเชื่อมต่ออินเทอร์เน็ต", offlineDesc: "ตรวจสอบการเชื่อมต่ออินเทอร์เน็ต", backendDisconnectedTitle: "เชื่อมต่อ Backend ไม่สำเร็จ", backendDisconnectedDesc: "ตรวจสอบว่า backend กำลังทำงาน (localhost:28888)" },
  dashboard: { tradingStopped: "หยุดเทรดแล้ว", tradingStarted: "เริ่มเทรดแล้ว", status: "สถานะ", running: "กำลังทำงาน", stopped: "หยุด", stop: "STOP", start: "START", totalPnL: "P&L รวม" },
  configuration: { title: "การตั้งค่า", saveChanges: "บันทึกการเปลี่ยนแปลง", saveSuccess: "บันทึกสำเร็จ", saveFailed: "บันทึกไม่สำเร็จ" },
  orders: { title: "การจัดการออเดอร์", pendingTab: "รอดำเนินการ", historyTab: "ประวัติ", noPendingOrders: "ไม่มีออเดอร์รอดำเนินการ", buy: "ซื้อ", sell: "ขาย", cancelOrder: "ยกเลิกออเดอร์", cancelAll: "ยกเลิกทั้งหมด" }
};

const ja = {
  common: { logout: "ログアウト", global: "グローバル", system: "システム", trading: "取引", version: "バージョン {{version}}", cancel: "キャンセル", close: "閉じる", save: "保存", confirm: "確認", loading: "読み込み中...", retry: "再試行", refresh: "更新", allExchanges: "すべての取引所", actions: "操作", error: "エラー" },
  sidebar: { overview: "概要", performanceMonitor: "パフォーマンス監視", eventCenter: "イベントセンター", runLogs: "実行ログ", aiPrompts: "AIプロンプト", newbieRiskCheck: "初心者リスクチェック", aiConfig: "AI設定", aiTasks: "AIタスク", strategyMarket: "戦略マーケット", capitalManagement: "資金管理", profitManagement: "損益管理", positionPlan: "ポジション計画", backtest: "バックテスト", tradingPanel: "取引パネル", currentPositions: "現在のポジション", orderManagement: "注文管理", strategySlots: "戦略スロット", strategyAllocation: "戦略配分", profitStatistics: "損益統計", reconciliation: "照合", riskMonitor: "リスク監視", klineDepth: "K線深度", fundingRate: "資金調達率", basisMonitor: "ベーシス監視", servicesStatus: "サービス状態", configManagement: "設定", firstTimeWizard: "初回セットアップウィザード", profile: "プロフィール", optimizer: "パラメータ最適化", newsAnalysis: "ニュース分析", dataExport: "データエクスポート", klineFiles: "K線ファイル", expandSidebar: "サイドバーを展開", collapseSidebar: "サイドバーを折りたたむ", groupMonitor: "監視と概要", groupBacktestData: "バックテストとデータ", groupAI: "AI", groupStrategyCapital: "戦略と資金" },
  login: { title: "ログイン", password: "パスワード", passwordPlaceholder: "パスワードを入力", passwordLogin: "パスワードでログイン", loading: "ログイン中...", enterPassword: "パスワードを入力", passwordError: "パスワードが正しくありません" },
  dashboard: { tradingStopped: "取引停止", tradingStarted: "取引開始", status: "状態", running: "実行中", stopped: "停止", stop: "停止", start: "開始", totalPnL: "総損益", tradingVolume: "取引量" },
  configuration: { title: "設定", saveChanges: "変更を保存", saveSuccess: "保存しました", saveFailed: "保存に失敗しました" },
  orders: { title: "注文管理", pendingTab: "未約定", historyTab: "履歴", noPendingOrders: "未約定の注文はありません", buy: "買い", sell: "売り", cancelOrder: "注文をキャンセル", cancelAll: "すべてキャンセル" },
  connection: { offlineTitle: "インターネット接続なし", offlineDesc: "インターネット接続を確認してください。", backendDisconnectedTitle: "バックエンド接続失敗", backendDisconnectedDesc: "バックエンドが動作していることを確認してください (localhost:28888)。" }
};

const tl = {
  common: { logout: "Logout", global: "Pandaigdig", system: "Sistema", trading: "Pangangalakal", version: "Bersyon {{version}}", cancel: "Kanselahin", close: "Isara", save: "I-save", confirm: "Kumpirmahin", loading: "Naglo-load...", retry: "Subukang muli", refresh: "I-refresh", allExchanges: "Lahat ng exchange", actions: "Mga aksyon", error: "Error" },
  sidebar: { overview: "Pangkalahatang-ideya", performanceMonitor: "Performance Monitor", eventCenter: "Event Center", runLogs: "Run Logs", aiPrompts: "AI Prompts", newbieRiskCheck: "Risk Check para sa Baguhan", aiConfig: "AI Config", aiTasks: "AI Tasks", strategyMarket: "Strategy Market", capitalManagement: "Capital Management", profitManagement: "Profit Management", backtest: "Backtest", tradingPanel: "Trading Panel", currentPositions: "Kasalukuyang Posisyon", orderManagement: "Order Management", configManagement: "Configuration", firstTimeWizard: "First-time Setup Wizard" },
  login: { title: "Mag-login", password: "Password", passwordPlaceholder: "Ilagay ang password", passwordLogin: "Login gamit ang Password", loading: "Naglo-login...", enterPassword: "Ilagay ang password", passwordError: "Maling password" },
  dashboard: { tradingStopped: "Huminto ang trading", tradingStarted: "Nagsimula ang trading", status: "Katayuan", running: "Tumatakbo", stopped: "Huminto", stop: "STOP", start: "START", totalPnL: "Kabuuang P&L", tradingVolume: "Volume ng Trading" },
  configuration: { title: "Mga Setting", saveChanges: "I-save ang mga pagbabago", saveSuccess: "Na-save na", saveFailed: "Hindi na-save" },
  orders: { title: "Order Management", pendingTab: "Nakabinbin", historyTab: "Kasaysayan", noPendingOrders: "Walang nakabibinging order", buy: "Bumili", sell: "Magbenta", cancelOrder: "Kanselahin ang order", cancelAll: "Kanselahin lahat" },
  connection: { offlineTitle: "Walang Internet", offlineDesc: "Suriin ang koneksyon sa Internet.", backendDisconnectedTitle: "Backend Disconnected", backendDisconnectedDesc: "Siguraduhing tumatakbo ang backend (localhost:28888)." }
};

const pl = {
  common: { logout: "Wyloguj", global: "Globalnie", system: "System", trading: "Handel", version: "Wersja {{version}}", cancel: "Anuluj", close: "Zamknij", save: "Zapisz", confirm: "Potwierdź", loading: "Ładowanie...", retry: "Ponów", refresh: "Odśwież", allExchanges: "Wszystkie giełdy", actions: "Akcje", error: "Błąd" },
  sidebar: { overview: "Przegląd", performanceMonitor: "Monitor wydajności", eventCenter: "Centrum zdarzeń", runLogs: "Logi uruchomienia", aiPrompts: "AI Prompts", newbieRiskCheck: "Kontrola ryzyka dla początkujących", aiConfig: "Konfiguracja AI", aiTasks: "Zadania AI", strategyMarket: "Rynek strategii", capitalManagement: "Zarządzanie kapitałem", profitManagement: "Zarządzanie zyskiem", backtest: "Backtest", tradingPanel: "Panel handlowy", currentPositions: "Aktualne pozycje", orderManagement: "Zarządzanie zleceniami", configManagement: "Konfiguracja", firstTimeWizard: "Kreator pierwszego uruchomienia" },
  login: { title: "Zaloguj", password: "Hasło", passwordPlaceholder: "Wprowadź hasło", passwordLogin: "Logowanie hasłem", loading: "Logowanie...", enterPassword: "Wprowadź hasło", passwordError: "Nieprawidłowe hasło" },
  dashboard: { tradingStopped: "Handel zatrzymany", tradingStarted: "Handel uruchomiony", status: "Status", running: "Działa", stopped: "Zatrzymany", stop: "STOP", start: "START", totalPnL: "Całkowity P&L", tradingVolume: "Wolumen handlu" },
  configuration: { title: "Ustawienia", saveChanges: "Zapisz zmiany", saveSuccess: "Zapisano pomyślnie", saveFailed: "Błąd zapisu" },
  orders: { title: "Zarządzanie zleceniami", pendingTab: "Oczekujące", historyTab: "Historia", noPendingOrders: "Brak oczekujących zleceń", buy: "Kup", sell: "Sprzedaj", cancelOrder: "Anuluj zlecenie", cancelAll: "Anuluj wszystkie" },
  connection: { offlineTitle: "Brak internetu", offlineDesc: "Sprawdź połączenie internetowe.", backendDisconnectedTitle: "Brak połączenia z backendem", backendDisconnectedDesc: "Upewnij się, że backend działa (localhost:28888)." }
};

function deepMerge(target, source) {
  for (const key of Object.keys(source)) {
    if (source[key] && typeof source[key] === 'object' && !Array.isArray(source[key])) {
      if (!target[key]) target[key] = {};
      deepMerge(target[key], source[key]);
    } else {
      target[key] = source[key];
    }
  }
}

const maps = { 'uk-UA': uk, 'ur-PK': ur, 'bn-BD': bn, 'th-TH': th, 'ja-JP': ja, 'tl-PH': tl, 'pl-PL': pl };
for (const [locale, tr] of Object.entries(maps)) {
  const fp = path.join(LOCALES, `${locale}.json`);
  const data = JSON.parse(fs.readFileSync(fp, 'utf8'));
  deepMerge(data, tr);
  fs.writeFileSync(fp, JSON.stringify(data, null, 2) + '\n', 'utf8');
  console.log(`${locale}: applied`);
}
