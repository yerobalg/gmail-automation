# gmail-automation

CLI that sends PDF attachments from local folders to Gmail recipients in batches, with a single confirmation prompt per recipient.

## How it works

1. You drop PDFs into `attachments/<recipient-email>/`.
2. The tool scans `attachments/`, treats each subfolder name as a recipient email, and groups PDFs into chunks based on `MAX_ATTACHMENTS_PER_EMAIL` and `MAX_SIZE_MB`.
3. It previews all emails for a recipient, asks once for confirmation, then sends them via Gmail SMTP.

## 1. Create a Gmail App Password

App Passwords are 16-character codes that let non-Google apps sign in to your Gmail. You need this because regular passwords no longer work for SMTP.

1. Make sure **2-Step Verification** is enabled on your Google account: <https://myaccount.google.com/security>.
2. Go to <https://myaccount.google.com/apppasswords>.
3. In the **App name** field, type anything (e.g. `gmail-automation`) and click **Create**.
4. Google shows a 16-character password (spaces are cosmetic — you can strip them). Copy it.
5. You will paste this into `.env` as `GMAIL_APP_PASSWORD` in the next step.

If you don't see the App Passwords page, your Google account either doesn't have 2-Step Verification on, or it's a Workspace account where the admin has disabled App Passwords.

## 2. Configure `.env`

From the project root:

```sh
cp .env.example .env
```

Edit `.env` and fill in:

| Variable | Description |
| --- | --- |
| `GMAIL_USER` | Your Gmail address (e.g. `you@gmail.com`). |
| `GMAIL_APP_PASSWORD` | The 16-character App Password from step 1. |
| `SENDER_NAME` | Name shown in the `From:` header and email signature. |
| `EMAIL_SUBJECT` | Subject line used for every email. |
| `ATTACHMENTS_ROOT` | Folder containing recipient subfolders. Defaults to `./attachments`. |
| `MAX_ATTACHMENTS_PER_EMAIL` | Max PDFs per email. Defaults to `5`. |
| `MAX_SIZE_MB` | Max total attachment size per email, in MB. Defaults to `15` (Gmail's per-message limit). |

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

A pre-built binary `gmail-automation` ships in this folder. No need to install Go.

**macOS / Linux** — from the project root:

```sh
./gmail-automation
```

If macOS blocks it the first time with "cannot be opened because the developer cannot be verified", either:

- Right-click the `gmail-automation` file in Finder → **Open** → confirm, **or**
- Run once: `xattr -d com.apple.quarantine gmail-automation`

If you get `permission denied`, mark it executable:

```sh
chmod +x gmail-automation
```

**Optional** — point at a different `.env`:

```sh
./gmail-automation -env /path/to/other.env
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

- **`GMAIL_USER and GMAIL_APP_PASSWORD are required`** — `.env` wasn't found or those keys are empty. Run from the project root, or pass `-env`.
- **`auth: ...535...`** — App Password is wrong, or 2-Step Verification was turned off after the password was issued. Generate a new one.
- **`folder "foo" is not a valid email address`** — rename the subfolder to a valid address, or remove it.
- **`file X exceeds per-email limit`** — a single PDF is larger than `MAX_SIZE_MB`. Either raise the limit or split the PDF.
