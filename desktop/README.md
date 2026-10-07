# NativePHP Notes (macOS)

Ứng dụng ghi chú Laravel 12 đóng gói thành app macOS bằng [NativePHP Desktop v2](https://nativephp.com/docs/desktop/2). PHP static binary (8.4) được bundle sẵn trong `.app`, người dùng không cần cài PHP.

## Yêu cầu dev

- PHP 8.4 (extension: sqlite3, pdo_sqlite, mbstring, intl, zip)
- Composer 2, Node 22+, macOS 12+ để build `.app`

## Chạy local

```bash
cd desktop
composer install
cp .env.example .env && php artisan key:generate
npm ci
php artisan native:install --no-interaction   # chỉ lần đầu
composer native:dev                            # native:run + vite
```

Chạy dạng web thường (không Electron): `php artisan migrate && php artisan serve`.

Test: `php artisan test`

## Dữ liệu

NativePHP dùng SQLite nằm trong thư mục app data (`~/Library/Application Support/<app>`), tự chạy migration khi khởi động. `storage_path()` cũng trỏ vào đó; không bao giờ ghi vào trong `.app`.

## Build macOS

```bash
php artisan native:build mac all     # arm64 + x64 -> nativephp/electron/dist/
php artisan native:build mac arm64   # chỉ Apple Silicon
```

Bản PHP được bundle trùng `major.minor` của PHP chạy lệnh build, nên luôn build bằng PHP 8.4.

**Cảnh báo:** file `.env` được copy vào bundle, ai cũng đọc được. Không để secret trong `.env` lúc build; các key khớp `cleanup_env_keys` trong `config/nativephp.php` sẽ bị xoá.

## CI / Release (`.github/workflows/desktop-macos.yml` ở root repo)

- Push/PR có thay đổi trong `desktop/**`: chạy test trên Ubuntu, build `.dmg`/`.zip` trên `macos-latest`, upload artifact.
- Tag `desktop-v*` (vd `desktop-v1.0.0`; tag `v*` dành cho release Rust của synca): version lấy từ tag, tạo GitHub Release dạng draft kèm `latest-mac.yml` cho auto-update.
- Không có secrets → build **unsigned**, không notarize, tắt auto-update (Gatekeeper sẽ chặn; mở bằng chuột phải > Open hoặc `xattr -dr com.apple.quarantine`).

### Secrets cần thêm (Settings → Secrets → Actions)

| Secret | Nội dung |
| --- | --- |
| `MACOS_CERTIFICATE_P12_BASE64` | Cert "Developer ID Application" export `.p12`, `base64 -i cert.p12` |
| `MACOS_CERTIFICATE_PASSWORD` | Mật khẩu file `.p12` |
| `NATIVEPHP_APPLE_ID` | Apple ID email |
| `NATIVEPHP_APPLE_ID_PASS` | App-specific password (appleid.apple.com) |
| `NATIVEPHP_APPLE_TEAM_ID` | Team ID (developer.apple.com → Membership) |

Có cert mà thiếu thông tin notarize thì build tag sẽ fail để tránh phát hành bản không notarize.

### Auto-update

Cấu hình sẵn provider `github` (`ngosangns/synca`) qua `NATIVEPHP_UPDATER_*`. Lưu ý: electron-updater lấy release *mới nhất* của repo, mà repo này cũng phát hành bản Rust `v*`; trước khi bật auto-update nên tách app ra repo riêng hoặc đổi sang provider khác (S3/Spaces). Chỉ bật khi app được ký (CI tự set `NATIVEPHP_UPDATER_ENABLED`). Publish draft release trên GitHub để app cũ nhận bản mới.

## Cấu trúc chính

- `app/Providers/NativeAppServiceProvider.php` — cửa sổ, menu macOS, php.ini
- `app/Http/Controllers/NoteController.php`, `resources/views/notes/index.blade.php` — CRUD ghi chú
- `config/nativephp.php` — app id, version, updater, cleanup env
