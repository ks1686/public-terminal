"""Public Terminal — entry point."""
from __future__ import annotations

import sys


def main() -> None:
    if "--rebalance" in sys.argv:
        from rebalance import rebalance
        rebalance()
    elif "--install-service" in sys.argv:
        from config import _install_service_files
        try:
            print(_install_service_files())
        except Exception as exc:
            print(f"Error: {exc}", file=sys.stderr)
            sys.exit(1)
    elif "--remove-service" in sys.argv:
        from config import _remove_service_files
        try:
            print(_remove_service_files())
        except Exception as exc:
            print(f"Error: {exc}", file=sys.stderr)
            sys.exit(1)
    else:
        from app import PublicTerminal
        PublicTerminal().run()


if __name__ == "__main__":
    main()
