"""Shared Public.com API client factory."""

from __future__ import annotations

import os

from dotenv import load_dotenv
from public_api_sdk import ApiKeyAuthConfig, PublicApiClient, PublicApiClientConfiguration

from config import ENV_FILE

load_dotenv(ENV_FILE)


def get_client() -> PublicApiClient:
    """Create and return an authenticated PublicApiClient.

    Both env vars are API secret keys — the SDK exchanges them for short-lived
    bearer tokens automatically via ApiKeyAuthConfig.

    Required for this app:
      PUBLIC_ACCOUNT_NUMBER — default account used by portfolio/order calls
    """
    access_token = os.environ.get("PUBLIC_ACCESS_TOKEN")
    api_secret_key = os.environ.get("PUBLIC_API_SECRET_KEY")

    if not access_token and not api_secret_key:
        raise RuntimeError(
            "No credentials found. Set PUBLIC_ACCESS_TOKEN or PUBLIC_API_SECRET_KEY in .env"
        )

    account_number = os.environ.get("PUBLIC_ACCOUNT_NUMBER")
    if not account_number:
        raise RuntimeError("No account number found. Set PUBLIC_ACCOUNT_NUMBER in .env")

    config = PublicApiClientConfiguration(default_account_number=account_number)

    secret = access_token or api_secret_key
    auth = ApiKeyAuthConfig(api_secret_key=secret)

    return PublicApiClient(auth_config=auth, config=config)
