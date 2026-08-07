import os
import socket


def resolve(fqdn: str) -> list[str]:
    """Resolve a vertexDomain FQDN to all IPv4 addresses via DNS.

    When a Vertex has multiple Pods, CoreDNS returns multiple A records.
    This function returns all unique IPs so callers can reach every Pod.
    """
    results = socket.getaddrinfo(fqdn, None, socket.AF_INET)
    seen: set[str] = set()
    ips: list[str] = []
    for r in results:
        ip = r[4][0]
        if ip not in seen:
            seen.add(ip)
            ips.append(ip)
    return ips


def resolve_targets(env_var: str = "VERTEX_DOMAIN_TARGETS") -> dict[str, list[str]]:
    """Read destination FQDNs from the env var injected by VertexDomainMutator
    and resolve each to all IPv4 addresses.

    Returns an empty dict when the env var is not set (e.g. on To-side Pods).
    """
    raw = os.environ.get(env_var, "")
    if not raw:
        return {}

    targets: dict[str, list[str]] = {}
    for fqdn in raw.split(","):
        targets[fqdn] = resolve(fqdn)
    return targets
