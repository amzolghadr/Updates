# appwatch

ابزار چک‌کردن آپدیت برنامه‌های گیت‌هابی و فارسرویدی، با اطلاع‌رسانی از طریق تلگرام.

## نصب و بیلد (روی ترموکس)

```bash
pkg install golang
cd appwatch
go build -o appwatch .
```

## ساخت بات تلگرام

1. توی تلگرام به @BotFather پیام بده و `/newbot` بزن، یه توکن می‌گیری.
2. یه پیام به بات خودت بفرست، بعد این آدرس رو باز کن تا chat_id رو پیدا کنی:
   `https://api.telegram.org/bot<TOKEN>/getUpdates`

## کانفیگ

فایل `config.example.json` رو کپی کن به `config.json` و پر کن:

```bash
cp config.example.json config.json
```

- `telegram_token`: توکن بات
- `chat_id`: عددی که از getUpdates گرفتی
- هر آیتم `apps`:
  - `source: "github"` + `repo: "owner/repo"`
  - `source: "farsroid"` + `url: "https://www.farsroid.com/..."`
  - `last_version` و `last_link` رو خالی بذار، خودش پر می‌کنه

اولین اجرا برای هر برنامه فقط baseline رو ثبت می‌کنه و نوتیف نمی‌فرسته (چون نسخه نصب‌شده روی گوشی رو نمی‌تونیم بدون روت بخونیم، این baseline جای اون رو می‌گیره).

## اجرا

```bash
./appwatch config.json
```

## زمان‌بندی خودکار (کرون در ترموکس)

```bash
pkg install cronie
crontab -e
```

و این خط رو اضافه کن (هر ۳ ساعت یک‌بار):

```
0 */3 * * * cd /path/to/appwatch && ./appwatch config.json >> appwatch.log 2>&1
```

سرویس کرون رو هم فعال کن:

```bash
crond
```

## نکته مهم درباره فارسروید

فارسروید API رسمی نداره، پس این بخش با استخراج از HTML صفحه کار می‌کنه (`farsroidVersionRe` و `farsroidLinkRe` توی `main.go`). اگه یه برنامه‌ی خاص رو درست تشخیص نداد، لاگش می‌گه کدوم regex باید تنظیم بشه - چون ساختار صفحه بین دسته‌بندی‌های مختلف فارسروید می‌تونه فرق داشته باشه.
