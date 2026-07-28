from graphify.privacy_cleanup import remove_legacy_logs


def test_remove_legacy_logs(monkeypatch, tmp_path):
    monkeypatch.setenv("HOME", str(tmp_path))
    cache = tmp_path / ".cache"
    cache.mkdir()
    query = cache / "graphify-queries.log"
    rebuild = cache / "graphify-rebuild.log"
    query.write_text("query")
    rebuild.write_text("rebuild")

    remove_legacy_logs()

    assert not query.exists()
    assert not rebuild.exists()
