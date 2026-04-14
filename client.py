"""Shared Public.com API client factory."""

from __future__ import annotations

import os

from dotenv import load_dotenv
from public_api_sdk import ApiKeyAuthConfig, PublicApiClient, PublicApiClientConfiguration

load_dotenv()


def get_client() -> PublicApiClient:
    """Create and return an authenticated PublicApiClient.

    Both env vars are API secret keys — the SDK exchanges them for short-lived
    bearer tokens automatically via ApiKeyAuthConfig.

    Optional:
      PUBLIC_ACCOUNT_NUMBER — default account so you don't pass it every call
    """
    access_token = os.environ.get("PUBLIC_ACCESS_TOKEN")
    api_secret_key = os.environ.get("PUBLIC_API_SECRET_KEY")

    if not access_token and not api_secret_key:
        raise RuntimeError(
            "No credentials found. Set PUBLIC_ACCESS_TOKEN or PUBLIC_API_SECRET_KEY in .env"
        )

    account_number = os.environ.get("PUBLIC_ACCOUNT_NUMBER")
    config = PublicApiClientConfiguration(default_account_number=account_number or None)

    secret = access_token or api_secret_key
    auth = ApiKeyAuthConfig(api_secret_key=secret)

    return PublicApiClient(auth_config=auth, config=config)
