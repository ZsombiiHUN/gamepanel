# Backups in S3

By default a server's backups are kept on its node, under the daemon's data directory. If the node's disk dies, its backups go with it. With an S3-compatible store, **new** backups go to a bucket instead.

Any store that speaks the S3 API works: AWS S3, Cloudflare R2, Backblaze B2, Wasabi, MinIO, RustFS, Garage and others.

## How it works

**The keys never leave the panel.** A node never gets your S3 credentials. When it makes a backup, the panel starts a multipart upload and gives the node presigned URLs, each allowing one `PUT` of one part for a limited time. The node streams the archive straight to the bucket; the data does not pass through the panel.

- **Downloads**: the panel sends the browser to a presigned URL valid for 15 minutes, so the download comes from the bucket directly.
- **Restores**: the node fetches the backup through a presigned URL.
- **Deletes and rotation** are done by the panel.

Objects are stored as `<prefix><server uuid>/<backup uuid>.tar.gz`.

Backups already on a node stay there and keep working. Turning S3 off again leaves the S3 backups in the bucket, but the panel can only reach them while S3 is configured.

## Setting it up

1. Create a bucket. Keep it **private**.
2. Create an access key that may only use this bucket. It needs to put, get and delete objects, and use multipart uploads (create, upload part, complete, abort).
3. Add to `/etc/gamepanel/panel.env`:

```ini
GAMEPANEL_S3_ENDPOINT=https://s3.eu-central-1.amazonaws.com
GAMEPANEL_S3_REGION=eu-central-1
GAMEPANEL_S3_BUCKET=example-gamepanel-backups
GAMEPANEL_S3_ACCESS_KEY=...
GAMEPANEL_S3_SECRET_KEY=...
GAMEPANEL_S3_PREFIX=backups/
```

4. Restart the panel. At start it writes and deletes a small test object (`<prefix>.gamepanel-check`). If that fails, it warns in the log and starts anyway; backups then fail with the reason until the store works.

Every node must be able to reach the S3 endpoint over HTTPS.

## Provider notes

| Provider | Endpoint | Region | Path style |
|---|---|---|---|
| AWS S3 | `https://s3.<region>.amazonaws.com` | the bucket's region | no |
| Cloudflare R2 | `https://<account id>.r2.cloudflarestorage.com` | `auto` | no |
| Backblaze B2 | `https://s3.<region>.backblazeb2.com` | e.g. `us-west-004` | no |
| Wasabi | `https://s3.<region>.wasabisys.com` | the bucket's region | no |
| MinIO, RustFS, Garage | your server's address | as configured (often `us-east-1`) | `GAMEPANEL_S3_PATH_STYLE=1` |

## Large backups

Parts are sized from the archive's size so that any realistic backup fits in S3's limit of 10,000 parts, while small backups go up in a single part. A backup has up to 2 hours to finish and the upload URLs are valid for 6 hours.

## Lifecycle rules

GamePanel deletes backups itself (when a user deletes one, rotates one out, or the server is deleted). If an upload is interrupted, the panel aborts it. As a safety net, add a lifecycle rule on the bucket that removes **incomplete multipart uploads** after a day or two; most providers offer one.

Do **not** add a rule that expires finished objects unless you want backups to vanish without the panel knowing.

## Tested with

`TestS3Backups` runs against a real S3-compatible store (RustFS) when `GAMEPANEL_TEST_S3_ENDPOINT` is set, and request signing is tested against AWS's published example.
