"""Tests for config.py schema migration and account management."""
from __future__ import annotations

import json
import os
import shutil
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch


def _patch_config_paths(tmp: Path):
    """Return a context manager that redirects all config paths into tmp."""
    accounts_file = tmp / "accounts.json"
    schema_file = tmp / "schema_version.json"
    accounts_dir = tmp / "accounts"
    env_file = tmp / ".env"
    import config
    return unittest.mock.patch.multiple(
        config,
        _APP_DIR=tmp,
        ACCOUNTS_FILE=accounts_file,
        SCHEMA_VERSION_FILE=schema_file,
        ACCOUNTS_DIR=accounts_dir,
        ENV_FILE=env_file,
        REBALANCE_CONFIG_FILE=tmp / "rebalance_config.json",
        CACHE_DIR=tmp / "cache",
    )


class TestMigration(unittest.TestCase):
    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp())

    def tearDown(self):
        shutil.rmtree(self.tmp)

    def test_v0_to_v1_moves_rebalance_config(self):
        import config
        env_file = self.tmp / ".env"
        env_file.write_text("PUBLIC_ACCESS_TOKEN=tok123\nPUBLIC_ACCOUNT_NUMBER=ACCT001\n")
        old_config = self.tmp / "rebalance_config.json"
        old_config.write_text(json.dumps({"index": "SP500", "top_n": 500}))

        with _patch_config_paths(self.tmp):
            os.environ["PUBLIC_ACCESS_TOKEN"] = "tok123"
            os.environ["PUBLIC_ACCOUNT_NUMBER"] = "ACCT001"
            config._migrate_v0_to_v1()

        new_config = self.tmp / "accounts" / "ACCT001" / "rebalance_config.json"
        self.assertTrue(new_config.exists())
        self.assertFalse(old_config.exists())

    def test_v0_to_v1_moves_cache_dir(self):
        import config
        env_file = self.tmp / ".env"
        env_file.write_text("PUBLIC_ACCESS_TOKEN=tok123\nPUBLIC_ACCOUNT_NUMBER=ACCT001\n")
        old_cache = self.tmp / "cache"
        old_cache.mkdir()
        (old_cache / "portfolio_cache.json").write_text("{}")

        with _patch_config_paths(self.tmp):
            os.environ["PUBLIC_ACCESS_TOKEN"] = "tok123"
            os.environ["PUBLIC_ACCOUNT_NUMBER"] = "ACCT001"
            config._migrate_v0_to_v1()

        new_cache = self.tmp / "accounts" / "ACCT001" / "cache"
        self.assertTrue(new_cache.exists())
        self.assertFalse(old_cache.exists())

    def test_v0_to_v1_rewrites_env_token_only(self):
        import config
        env_file = self.tmp / ".env"
        env_file.write_text("PUBLIC_ACCESS_TOKEN=tok123\nPUBLIC_ACCOUNT_NUMBER=ACCT001\n")

        with _patch_config_paths(self.tmp):
            os.environ["PUBLIC_ACCESS_TOKEN"] = "tok123"
            os.environ["PUBLIC_ACCOUNT_NUMBER"] = "ACCT001"
            config._migrate_v0_to_v1()

        content = env_file.read_text()
        self.assertIn("PUBLIC_ACCESS_TOKEN=tok123", content)
        self.assertNotIn("PUBLIC_ACCOUNT_NUMBER", content)

    def test_v0_to_v1_writes_accounts_json(self):
        import config
        env_file = self.tmp / ".env"
        env_file.write_text("PUBLIC_ACCESS_TOKEN=tok123\nPUBLIC_ACCOUNT_NUMBER=ACCT001\n")

        with _patch_config_paths(self.tmp):
            os.environ["PUBLIC_ACCESS_TOKEN"] = "tok123"
            os.environ["PUBLIC_ACCOUNT_NUMBER"] = "ACCT001"
            config._migrate_v0_to_v1()

        accounts = json.loads((self.tmp / "accounts.json").read_text())
        self.assertEqual(accounts, ["ACCT001"])

    def test_migrate_if_needed_skips_when_accounts_dir_exists(self):
        import config
        accounts_dir = self.tmp / "accounts"
        accounts_dir.mkdir()

        with _patch_config_paths(self.tmp):
            config.migrate_if_needed()

        schema = json.loads((self.tmp / "schema_version.json").read_text())
        self.assertEqual(schema["version"], config.CURRENT_SCHEMA_VERSION)

    def test_migrate_if_needed_noop_when_current(self):
        import config
        schema_file = self.tmp / "schema_version.json"
        schema_file.write_text(json.dumps({"version": config.CURRENT_SCHEMA_VERSION}))

        with _patch_config_paths(self.tmp):
            config.migrate_if_needed()  # should not raise


class TestAccountCRUD(unittest.TestCase):
    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp())

    def tearDown(self):
        shutil.rmtree(self.tmp)

    def test_get_accounts_empty_when_missing(self):
        import config
        with _patch_config_paths(self.tmp):
            self.assertEqual(config.get_accounts(), [])

    def test_add_account_creates_entry_and_dir(self):
        import config
        with _patch_config_paths(self.tmp):
            config.add_account("ACCT001")
            self.assertEqual(config.get_accounts(), ["ACCT001"])
            self.assertTrue((self.tmp / "accounts" / "ACCT001").exists())

    def test_add_account_deduplicates(self):
        import config
        with _patch_config_paths(self.tmp):
            config.add_account("ACCT001")
            config.add_account("ACCT001")
            self.assertEqual(config.get_accounts(), ["ACCT001"])

    def test_add_account_normalizes_case(self):
        import config
        with _patch_config_paths(self.tmp):
            config.add_account("acct001")
            self.assertEqual(config.get_accounts(), ["ACCT001"])

    def test_remove_account_deletes_dir(self):
        import config
        with _patch_config_paths(self.tmp):
            config.add_account("ACCT001")
            config.add_account("ACCT002")
            config.remove_account("ACCT001")
            self.assertNotIn("ACCT001", config.get_accounts())
            self.assertFalse((self.tmp / "accounts" / "ACCT001").exists())

    def test_remove_last_account_raises(self):
        import config
        with _patch_config_paths(self.tmp):
            config.add_account("ACCT001")
            with self.assertRaises(ValueError):
                config.remove_account("ACCT001")

    def test_account_scoped_paths_use_correct_dirs(self):
        import config
        with _patch_config_paths(self.tmp):
            p = config.get_rebalance_config_path("ACCT001")
            self.assertEqual(p, self.tmp / "accounts" / "ACCT001" / "rebalance_config.json")
            p2 = config.get_portfolio_cache_path("ACCT001")
            self.assertEqual(p2, self.tmp / "accounts" / "ACCT001" / "cache" / "portfolio_cache.json")


if __name__ == "__main__":
    unittest.main()
