import socket
from unittest.mock import patch

import pytest

from vertexdomain import resolve, resolve_targets


class TestResolve:
    def test_success(self):
        mock_result = [(socket.AF_INET, socket.SOCK_STREAM, 6, "", ("192.168.140.10", 0))]
        with patch("vertexdomain.resolver.socket.getaddrinfo", return_value=mock_result):
            ip = resolve("vertex-in.pipeline1.default.vertexdomain.local")
        assert ip == "192.168.140.10"

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
        assert targets == {"inference.my-pipeline.default.vertexdomain.local": "10.0.0.1"}

    def test_multiple_targets(self, monkeypatch):
        fqdns = "a.p.ns.vertexdomain.local,b.p.ns.vertexdomain.local"
        monkeypatch.setenv("VERTEX_DOMAIN_TARGETS", fqdns)

        def fake_getaddrinfo(fqdn, port, family):
            ips = {
                "a.p.ns.vertexdomain.local": "10.0.0.1",
                "b.p.ns.vertexdomain.local": "10.0.0.2",
            }
            return [(family, socket.SOCK_STREAM, 6, "", (ips[fqdn], 0))]

        with patch("vertexdomain.resolver.socket.getaddrinfo", side_effect=fake_getaddrinfo):
            targets = resolve_targets()
        assert targets == {
            "a.p.ns.vertexdomain.local": "10.0.0.1",
            "b.p.ns.vertexdomain.local": "10.0.0.2",
        }

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
