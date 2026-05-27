# gmail-automation

CLI that sends PDF attachments from local folders to email recipients in batches, with a single confirmation prompt per recipient. Works with Gmail / Google Workspace out of the box, and with any other SMTP provider (Microsoft 365, Zoho, Zimbra, custom company servers) via config.

## How it works

1. You drop PDFs into `attachments/<recipient-email>/`.
2. The tool scans `attachments/`, treats each subfolder name as a recipient email, and groups PDFs into chunks based on `MAX_ATTACHMENTS_PER_EMAIL` and `MAX_SIZE_MB`.
3. It previews all emails for a recipient, asks once for confirmation, then sends them via your SMTP server.

## 1. Get a sender password

### Gmail / Google Workspace

You need an **App Password** — regular Gmail passwords don't work for SMTP anymore.

1. Make sure **2-Step Verification** is enabled on your Google account: <https://myaccount.google.com/security>.
2. Go to <https://myaccount.google.com/apppasswords>.
3. In the **App name** field, type anything (e.g. `gmail-automation`) and click **Create**.
4. Google shows a 16-character password (spaces are cosmetic — you can strip them). Copy it.
5. You will paste this into `.env` as `SMTP_PASSWORD` in the next step.

If you don't see the App Passwords page, your Google account either doesn't have 2-Step Verification on, or it's a Workspace account where the admin has disabled App Passwords.

### Other providers (Microsoft 365, Zoho, Zimbra, custom)

Usually your normal mailbox password works. A few notes:

- **Microsoft 365**: if the account has MFA on, you need an App Password (Microsoft has its own equivalent — search "Microsoft App Password" in your account settings) or your admin must allow SMTP AUTH.
- **Zimbra**: uses the regular mailbox password. If the account has 2FA, ask the admin to enable Zimbra's *application passwords* feature, or use a non-2FA account.
- **Custom company server**: ask IT for the SMTP host, port, and what credential to use.

## 2. Configure `.env`

From the project root:

```sh
cp .env.example .env
```

Edit `.env` and fill in:

| Variable | Description |
| --- | --- |
| `SMTP_USER` | Sender email address (e.g. `you@gmail.com` or `you@company.co.id`). |
| `SMTP_PASSWORD` | Sender password — App Password for Gmail, mailbox password for most others. |
| `SENDER_NAME` | Name shown in the `From:` header and email signature. |
| `EMAIL_SUBJECT` | Subject line used for every email. |
| `ATTACHMENTS_ROOT` | Folder containing recipient subfolders. Defaults to `./attachments`. |
| `MAX_ATTACHMENTS_PER_EMAIL` | Max PDFs per email. Defaults to `5`. |
| `MAX_SIZE_MB` | Max total attachment size per email, in MB. Defaults to `15` (Gmail's per-message limit). |
| `SMTP_HOST` | SMTP server hostname. Defaults to `smtp.gmail.com`. |
| `SMTP_PORT` | SMTP port. Defaults to `465` (implicit TLS). Use `587` for STARTTLS. |

### SMTP settings by provider

| Provider | `SMTP_HOST` | `SMTP_PORT` |
| --- | --- | --- |
| Gmail / Google Workspace | `smtp.gmail.com` | `465` |
| Microsoft 365 / Outlook | `smtp.office365.com` | `587` |
| Zoho | `smtp.zoho.com` | `465` |
| Zimbra (self-hosted) | ask IT (often `mail.<company>`) | `465` or `587` |
| Custom company server | ask IT | ask IT |

The tool picks the TLS mode automatically: port `465` uses implicit TLS, anything else uses STARTTLS.

## 3. Organize attachments

Each recipient gets a subfolder named after their email address. Folder names must be valid email addresses or the run will abort.

```
attachments/
├── alice@example.com/
│   ├── invoice-001.pdf
│   └── invoice-002.pdf
└── bob@example.com/
    └── invoice-003.pdf
```

Only files with the `.pdf` extension are picked up. PDFs are sorted alphabetically before chunking.

## 4. Run the program

Pre-built binaries for every major platform ship in [`dist/`](dist/). No need to install Go. Pick the one that matches your machine:

| Platform | Binary |
| --- | --- |
| macOS, Apple Silicon (M1/M2/M3/M4) | `dist/gmail-automation-macos-arm64` |
| macOS, Intel | `dist/gmail-automation-macos-amd64` |
| Linux x86_64 | `dist/gmail-automation-linux-amd64` |
| Linux ARM64 (Raspberry Pi 4/5, AWS Graviton, etc.) | `dist/gmail-automation-linux-arm64` |
| Windows 64-bit | `dist/gmail-automation-windows-amd64.exe` |

Not sure which Mac you have? Apple menu → **About This Mac** — anything with an "M" chip is Apple Silicon, anything labeled "Intel" is `amd64`. On Linux: `uname -m` (returns `x86_64` for amd64 or `aarch64` for arm64).

Copy the binary you need next to your `.env` file, then run it from that folder.

### macOS / Linux

```sh
chmod +x gmail-automation-macos-arm64   # one-time, if you see "permission denied"
./gmail-automation-macos-arm64
```

If macOS blocks it the first time with *"cannot be opened because the developer cannot be verified"*, either:

- Right-click the binary in Finder → **Open** → confirm in the dialog, **or**
- Run once: `xattr -d com.apple.quarantine gmail-automation-macos-arm64`

### Windows

Double-click `gmail-automation-windows-amd64.exe`, or run from PowerShell / cmd:

```powershell
.\gmail-automation-windows-amd64.exe
```

Windows SmartScreen may show *"Windows protected your PC"* because the binary isn't code-signed. Click **More info** → **Run anyway**.

### Optional flag

Point at a `.env` file in a different location:

```sh
./gmail-automation-macos-arm64 -env /path/to/other.env
```

## 5. Confirm and send

For each recipient, the tool prints a preview of every email it plans to send (recipient, subject, attachment list, body), then asks:

```
Send all N email(s) to alice@example.com? [y/N]:
```

- Type `y` (or `yes`) to send every email for that recipient.
- Anything else skips the whole batch for that recipient and moves on.

After each send you'll see `email #1 sent`, `email #2 sent`, etc. At the end you get a summary: `Sent: X, Skipped: Y, Failed: Z`.

## Troubleshooting

- **`SMTP_USER and SMTP_PASSWORD are required`** — `.env` wasn't found or those keys are empty. Run from the project root, or pass `-env`.
- **`auth: ...535...`** — Password is wrong, or (Gmail) 2-Step Verification was turned off after the App Password was issued. Generate a new one.
- **`tls dial ...`** / **`dial ...`** — wrong `SMTP_HOST` / `SMTP_PORT`, or the server is not reachable from your network. Some Zimbra installs block SMTP submission from outside the office LAN.
- **`server ... does not advertise STARTTLS on port ...`** — you set a non-465 port on a server that doesn't support STARTTLS. Try port `465` instead, or confirm with IT.
- **`folder "foo" is not a valid email address`** — rename the subfolder to a valid address, or remove it.
- **`file X exceeds per-email limit`** — a single PDF is larger than `MAX_SIZE_MB`. Either raise the limit or split the PDF.
