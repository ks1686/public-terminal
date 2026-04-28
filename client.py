"""Shared Public.com API client factory."""

from __future__ import annotations

import os
from dataclasses import dataclass

from dotenv import load_dotenv
from public_api_sdk import (
    ApiKeyAuthConfig,
    InstrumentType,
    OrderSide,
    PublicApiClient,
    PublicApiClientConfiguration,
)

from config import ENV_FILE

load_dotenv(ENV_FILE)


@dataclass(frozen=True)
class InstrumentLookup:
    """Raw Public instrument lookup result.

    The SDK's typed instrument models currently reject some live Public API
    responses because instrumentDetails is shape-specific. This app only needs
    stable top-level tradability fields, so we read those from the raw response.
    """

    symbol: str
    instrument_type: InstrumentType
    trading: str
    fractional_trading: str

    @property
    def is_buyable(self) -> bool:
        return self.trading == "BUY_AND_SELL"

    @property
    def is_sellable(self) -> bool:
        return self.trading in {"BUY_AND_SELL", "LIQUIDATION_ONLY"}


def get_client(account_id: str) -> PublicApiClient:
    """Create and return an authenticated PublicApiClient for the given account."""
    access_token = os.environ.get("PUBLIC_ACCESS_TOKEN")
    api_secret_key = os.environ.get("PUBLIC_API_SECRET_KEY")

    if not access_token and not api_secret_key:
        raise RuntimeError("No credentials found. Set PUBLIC_ACCESS_TOKEN in .env")

    if not account_id or not account_id.strip():
        raise RuntimeError("account_id must be a non-empty string.")

    cfg = PublicApiClientConfiguration(default_account_number=account_id.upper().strip())
    secret = access_token or api_secret_key
    auth = ApiKeyAuthConfig(api_secret_key=secret)
    return PublicApiClient(auth_config=auth, config=cfg)


def get_instrument_lookup(
    client: PublicApiClient, symbol: str, instrument_type: InstrumentType
) -> InstrumentLookup:
    """Return top-level Public instrument tradability fields using the raw API."""
    clean_symbol = symbol.upper().strip()
    client.auth_manager.refresh_token_if_needed()
    response = client.api_client.get(
        f"/userapigateway/trading/instruments/{clean_symbol}/{instrument_type.value}"
    )
    instrument = response.get("instrument") or {}
    return InstrumentLookup(
        symbol=str(instrument.get("symbol") or clean_symbol),
        instrument_type=InstrumentType(instrument.get("type") or instrument_type.value),
        trading=str(response.get("trading") or "DISABLED"),
        fractional_trading=str(response.get("fractionalTrading") or "DISABLED"),
    )


def get_tradable_instrument_symbols(
    client: PublicApiClient,
    instrument_type: InstrumentType,
    side: OrderSide,
) -> set[str]:
    """Return Public symbols tradable for the requested side using the raw API."""
    allowed_trading = (
        {"BUY_AND_SELL"}
        if side == OrderSide.BUY
        else {"BUY_AND_SELL", "LIQUIDATION_ONLY"}
    )
    client.auth_manager.refresh_token_if_needed()
    response = client.api_client.get(
        "/userapigateway/trading/instruments",
        params={
            "typeFilter": [instrument_type.value],
            "tradingFilter": sorted(allowed_trading),
        },
    )
    symbols: set[str] = set()
    for item in response.get("instruments") or []:
        if str(item.get("trading") or "") not in allowed_trading:
            continue
        instrument = item.get("instrument") or {}
        if instrument.get("type") != instrument_type.value:
            continue
        symbol = str(instrument.get("symbol") or "").upper().strip()
        if symbol:
            symbols.add(symbol)
    return symbols


def validate_order_instrument(
    client: PublicApiClient,
    symbol: str,
    instrument_type: InstrumentType,
    side: OrderSide,
) -> InstrumentLookup:
    """Raise ValueError unless Public reports the symbol as tradable for this side."""
    try:
        lookup = get_instrument_lookup(client, symbol, instrument_type)
    except Exception as exc:
        raise ValueError(
            f"{symbol.upper().strip()} {instrument_type.value} was not found in Public instruments"
        ) from exc

    if side == OrderSide.BUY and not lookup.is_buyable:
        raise ValueError(
            f"{lookup.symbol} {lookup.instrument_type.value} is not buyable "
            f"(trading={lookup.trading})"
        )
    if side == OrderSide.SELL and not lookup.is_sellable:
        raise ValueError(
            f"{lookup.symbol} {lookup.instrument_type.value} is not sellable "
            f"(trading={lookup.trading})"
        )
    return lookup
