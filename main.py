"""Public Terminal — entry point."""
from __future__ import annotations

import sys


def main() -> None:
    args = sys.argv[1:]
    if "--rebalance" in args:
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
