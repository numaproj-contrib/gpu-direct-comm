import socket
from unittest.mock import patch

import pytest

from vertexdomain import resolve, resolve_targets


class TestResolve:
    def test_success_returns_list(self):
        mock_result = [(socket.AF_INET, socket.SOCK_STREAM, 6, "", ("192.168.140.10", 0))]
        with patch("vertexdomain.resolver.socket.getaddrinfo", return_value=mock_result):
            ips = resolve("vertex-in.pipeline1.default.vertexdomain.local")
        assert ips == ["192.168.140.10"]

    def test_multiple_ips(self):
        mock_result = [
            (socket.AF_INET, socket.SOCK_STREAM, 6, "", ("10.0.0.1", 0)),
            (socket.AF_INET, socket.SOCK_STREAM, 6, "", ("10.0.0.2", 0)),
            (socket.AF_INET, socket.SOCK_STREAM, 6, "", ("10.0.0.3", 0)),
        ]
        with patch("vertexdomain.resolver.socket.getaddrinfo", return_value=mock_result):
            ips = resolve("vertex-in.pipeline1.default.vertexdomain.local")
        assert sorted(ips) == ["10.0.0.1", "10.0.0.2", "10.0.0.3"]

    def test_deduplicates_ips(self):
        mock_result = [
            (socket.AF_INET, socket.SOCK_STREAM, 6, "", ("10.0.0.1", 0)),
            (socket.AF_INET, socket.SOCK_DGRAM, 17, "", ("10.0.0.1", 0)),
            (socket.AF_INET, socket.SOCK_STREAM, 6, "", ("10.0.0.2", 0)),
        ]
        with patch("vertexdomain.resolver.socket.getaddrinfo", return_value=mock_result):
            ips = resolve("vertex-in.pipeline1.default.vertexdomain.local")
        assert sorted(ips) == ["10.0.0.1", "10.0.0.2"]

    def test_failure_raises(self):
        with patch(
            "vertexdomain.resolver.socket.getaddrinfo",
            side_effect=socket.gaierror("Name does not resolve"),
        ):
            with pytest.raises(socket.gaierror):
                resolve("nonexistent.vertexdomain.local")


class TestResolveTargets:
    def test_single_target(self, monkeypatch):
        monkeypatch.setenv("VERTEX_DOMAIN_TARGETS", "inference.my-pipeline.default.vertexdomain.local")
        mock_result = [(socket.AF_INET, socket.SOCK_STREAM, 6, "", ("10.0.0.1", 0))]
        with patch("vertexdomain.resolver.socket.getaddrinfo", return_value=mock_result):
            targets = resolve_targets()
        assert targets == {"inference.my-pipeline.default.vertexdomain.local": ["10.0.0.1"]}

    def test_multiple_targets(self, monkeypatch):
        fqdns = "a.p.ns.vertexdomain.local,b.p.ns.vertexdomain.local"
        monkeypatch.setenv("VERTEX_DOMAIN_TARGETS", fqdns)

        def fake_getaddrinfo(fqdn, port, family):
            ips = {
                "a.p.ns.vertexdomain.local": [("10.0.0.1", 0)],
                "b.p.ns.vertexdomain.local": [("10.0.0.2", 0)],
            }
            return [(family, socket.SOCK_STREAM, 6, "", addr) for addr in ips[fqdn]]

        with patch("vertexdomain.resolver.socket.getaddrinfo", side_effect=fake_getaddrinfo):
            targets = resolve_targets()
        assert targets == {
            "a.p.ns.vertexdomain.local": ["10.0.0.1"],
            "b.p.ns.vertexdomain.local": ["10.0.0.2"],
        }

    def test_multiple_ips_per_target(self, monkeypatch):
        monkeypatch.setenv("VERTEX_DOMAIN_TARGETS", "vertex.p.ns.vertexdomain.local")

        mock_result = [
            (socket.AF_INET, socket.SOCK_STREAM, 6, "", ("10.0.0.1", 0)),
            (socket.AF_INET, socket.SOCK_STREAM, 6, "", ("10.0.0.2", 0)),
        ]
        with patch("vertexdomain.resolver.socket.getaddrinfo", return_value=mock_result):
            targets = resolve_targets()
        assert targets == {"vertex.p.ns.vertexdomain.local": ["10.0.0.1", "10.0.0.2"]}

    def test_env_not_set(self, monkeypatch):
        monkeypatch.delenv("VERTEX_DOMAIN_TARGETS", raising=False)
        targets = resolve_targets()
        assert targets == {}

    def test_dns_failure_raises(self, monkeypatch):
        monkeypatch.setenv("VERTEX_DOMAIN_TARGETS", "bad.vertexdomain.local")
        with patch(
            "vertexdomain.resolver.socket.getaddrinfo",
            side_effect=socket.gaierror("Name does not resolve"),
        ):
            with pytest.raises(socket.gaierror):
                resolve_targets()
