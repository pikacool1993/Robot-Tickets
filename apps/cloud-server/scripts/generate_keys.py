#!/usr/bin/env python3
"""Generate local development keys for cloud-server.

The values are generated from 32 random bytes and then Base64 encoded.
"""

import base64
import json
import secrets


def new_key() -> str:
    return base64.b64encode(secrets.token_bytes(32)).decode("ascii")


def main() -> None:
    client_key = new_key()
    credential_key = new_key()

    transport_keys = json.dumps(
        {"client-v1": client_key},
        separators=(",", ":"),
    )

    print("# Copy these lines into apps/cloud-server/.env")
    print("TRANSPORT_KEYS_JSON=" + transport_keys)
    print("AGENT_TRANSPORT_KEY_ID=client-v1")
    print("AGENT_CREDENTIAL_ENCRYPTION_KEY=" + credential_key)
    print("AGENT_CREDENTIAL_KEY_VERSION=v1")
    print()
    print("# Keep these values private and do not commit .env")


if __name__ == "__main__":
    main()
