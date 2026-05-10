"""Public Terminal — entry point."""
from __future__ import annotations

import sys


def _get_installed_version() -> str | None:
    """Return installed distribution version if available, else None."""
    try:
        import importlib.metadata as md
    except Exception:
        return None
    for name in ("public-terminal", "public_terminal", "public_terminal"):
        try:
            return md.version(name)
        except Exception:
            continue
    return None


def _version_from_pyproject() -> str | None:
    """Fallback: parse pyproject.toml for project.version."""
    try:
        import re
        import os

        # Try to find pyproject.toml in parent directories
        paths_to_try = [
            "pyproject.toml",  # Current directory
            "../pyproject.toml",  # Parent
            os.path.join(os.path.dirname(__file__), "pyproject.toml"),  # Script dir
        ]
        
        for path in paths_to_try:
            if os.path.isfile(path):
                with open(path, "r", encoding="utf-8") as f:
                    for line in f:
                        m = re.match(r"\s*version\s*=\s*\"(.+?)\"", line)
                        if m:
                            return m.group(1)
    except Exception:
        return None


def main() -> None:
    args = sys.argv[1:]

    # Support --version early and exit
    if "--version" in args or "-v" in args:
        ver = _get_installed_version()
        if not ver:
            ver = _version_from_pyproject() or "unknown"
        print(ver)
        return

    if "--rebalance" in args:
        from config import migrate_if_needed
        migrate_if_needed()
        account_id = None
        if "--account" in args:
            idx = args.index("--account")
            if idx + 1 < len(args):
                account_id = args[idx + 1]
        from rebalance import rebalance
        rebalance(dry_run="--dry-run" in args, account_id=account_id)
    elif "--install-service" in args:
        from config import _install_service_files
        try:
            print(_install_service_files())
        except Exception as exc:
            print(f"Error: {exc}", file=sys.stderr)
            sys.exit(1)
    elif "--remove-service" in args:
        from config import _remove_service_files
        try:
            print(_remove_service_files())
        except Exception as exc:
            print(f"Error: {exc}", file=sys.stderr)
            sys.exit(1)
    else:
        from config import migrate_if_needed
        migrate_if_needed()
        from app import PublicTerminal
        PublicTerminal().run()


if __name__ == "__main__":
    main()
