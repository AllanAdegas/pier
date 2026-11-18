Use a unique password and HTTPS on the panel. API tokens do not replace a browser session for managing the security of your own account.

## Turn on 2FA

1. Sign in to the panel and open **Security**, in the account section.
2. Start the two-factor authentication setup. Scan the QR code with an authenticator app, or copy the key manually. The QR code is generated locally.
3. Enter the six-digit code to confirm.
4. Keep the ten recovery codes off the server, in a safe place. They are shown only when generated; each code works once.
5. At the next sign-in, enter your password and then the authenticator code or a recovery code.

If the code fails, synchronize the clock of your phone and of the server. A code that was already used cannot be repeated. Generating new recovery codes replaces the previous set; keep the new one. Turning off 2FA requires the password and the second factor.

## Recover the password

**Forgot my password** only appears when the instance has a public URL and an email channel configured. The link expires in 30 minutes and works once. The response does not confirm whether the email exists. Resetting the password ends sessions but keeps 2FA: keep your recovery codes.

Under **Security › Your account › Account sessions**, check the IP, browser and last activity. Revoke unknown sessions or use the option to close the other sessions. There is no guaranteed geographic location from the IP.

## Offline recovery by the operator

Without email or without a second factor, the operator with access to the data can use the `pier` binary that matches the installation, with the instance stopped. The commands refuse to run while the database is in use. `DIRETORIO` must contain the existing database; it is not a new installation.

```sh
pier admin reset-password --data-dir DIRETORIO --email EMAIL --password-file ARQUIVO
pier admin disable-2fa --data-dir DIRETORIO --email EMAIL
```

Create the password file with restricted access, with no password in the argument or in an environment variable, and remove it after use. Run only the command you need. Turning off 2FA also revokes sessions. This offline access has administrative power; protect the host and the backups.

For corporate sign-in, see [Team and SSO](/en/docs/team-and-access/).
