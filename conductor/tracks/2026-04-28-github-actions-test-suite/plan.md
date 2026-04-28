# GitHub Actions Test Suite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Develop a fully automated, comprehensive GitHub Actions test suite covering linting, security scanning, unit testing, and UI testing, ensuring no real orders are placed.

**Architecture:** We will transition from `unittest` to `pytest` for better coverage reporting and plugin support. The CI pipeline will be expanded to include `ruff` (linting/formatting), `bandit` (security), and `pytest-cov` (coverage). We will also add a dedicated test file (`test_app.py`) for the Textual UI, utilizing `unittest.mock` to prevent real brokerage API interactions.

**Tech Stack:** GitHub Actions, Python (pytest, pytest-cov, ruff, bandit, textual testing).

---

### Task 1: Update Development Dependencies

**Files:**
- Modify: `pyproject.toml`

- [ ] **Step 1: Add new dev dependencies**

Add `pytest`, `pytest-cov`, `pytest-asyncio`, `ruff`, and `bandit` to the `[dependency-groups]` section in `pyproject.toml`.

```toml
[dependency-groups]
dev = [
    "bandit>=1.7.7",
    "pyinstaller>=6.19.0",
    "pytest>=8.0.0",
    "pytest-asyncio>=0.23.0",
    "pytest-cov>=4.1.0",
    "ruff>=0.2.0",
]
```

- [ ] **Step 2: Commit**

```bash
git add pyproject.toml uv.lock
git commit -m "build(deps): add pytest, ruff, and bandit to dev dependencies"
```

### Task 2: Create UI Tests (Ensuring No Real Orders)

**Files:**
- Create: `test_app.py`

- [ ] **Step 1: Write the Textual UI test**

Create `test_app.py` to test the UI flow using Textual's async testing framework and `unittest.mock` to stub the `Client` completely.

```python
import pytest
from unittest.mock import patch, MagicMock
from app import PublicTerminalApp
from public_api_sdk import OrderStatus, OrderSide

@pytest.mark.asyncio
async def test_app_startup_and_mocked_portfolio():
    # Create a completely mocked client to ensure no real network or order calls
    mock_client = MagicMock()
    mock_portfolio = MagicMock()
    mock_portfolio.equity = "10000.00"
    mock_portfolio.cash = "500.00"
    mock_portfolio.positions = []
    mock_portfolio.orders = []
    mock_client.get_portfolio.return_value = mock_portfolio
    mock_client.get_accounts.return_value = [{"accountNumber": "TEST_ACCT"}]

    with patch("app.Client", return_value=mock_client), \
         patch("app.Config", return_value=MagicMock()), \
         patch("app.get_client", return_value=mock_client):

        app = PublicTerminalApp()
        async with app.run_test() as pilot:
            # Verify the app starts up and renders the mocked equity
            assert app.title == "Public Terminal"
            await pilot.pause()
```

- [ ] **Step 2: Commit**

```bash
git add test_app.py
git commit -m "test: add mocked UI tests for textual app"
```

### Task 3: Overhaul GitHub Actions Workflow

**Files:**
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Replace the CI workflow content**

Update `.github/workflows/ci.yml` to include the new checks and run `pytest` with coverage.

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

env:
  PYTHON_VERSION: "3.12"

jobs:
  test:
    runs-on: ubuntu-latest

    steps:
      - uses: actions/checkout@v4

      - name: Install uv
        uses: astral-sh/setup-uv@v5

      - name: Set up Python
        run: uv python install ${{ env.PYTHON_VERSION }}

      - name: Install dependencies
        run: uv sync --all-groups

      - name: Run Ruff Linting
        run: uv run ruff check .

      - name: Run Bandit Security Scan
        run: uv run bandit -r . -c "pyproject.toml" -ll -ii

      - name: Compile check
        run: |
          uv run python -m compileall app.py config.py widgets.py modals.py rebalance.py main.py client.py test_audit.py test_rebalance.py test_config.py test_app.py

      - name: Import smoke check
        run: |
          uv run python -c "import app, client, config, main, modals, rebalance, widgets"

      - name: Run Pytest with Coverage
        run: uv run pytest --cov=. --cov-report=xml --cov-report=term-missing -v
        env:
          PUBLIC_ACCESS_TOKEN: "mock_token_for_tests"
          PUBLIC_ACCOUNT_NUMBER: "mock_account_for_tests"
```

- [ ] **Step 2: Add Bandit Config to pyproject.toml**

Append Bandit configuration to `pyproject.toml` to ignore the test files.

```toml
[tool.bandit]
exclude_dirs = ["tests", "test_*.py"]
skips = ["B101"] # Skip assert checks as we use them in pytest
```

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/ci.yml pyproject.toml
git commit -m "ci: overhaul github actions with pytest, ruff, bandit, and coverage"
```
