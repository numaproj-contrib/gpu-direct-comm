import os
import socket


def resolve(fqdn: str) -> str:
    """Resolve a vertexDomain FQDN to an IPv4 address via DNS.

    Uses the cluster CoreDNS which serves the vertexdomain.local zone
    via its etcd plugin (ADR-002).
    """
    results = socket.getaddrinfo(fqdn, None, socket.AF_INET)
    return results[0][4][0]


def resolve_targets(env_var: str = "VERTEX_DOMAIN_TARGETS") -> dict[str, str]:
    """Read destination FQDNs from the env var injected by VertexDomainMutator
    and resolve each to an IPv4 address.

    Returns an empty dict when the env var is not set (e.g. on To-side Pods).
    """
    raw = os.environ.get(env_var, "")
    if not raw:
        return {}

    targets: dict[str, str] = {}
    for fqdn in raw.split(","):
        targets[fqdn] = resolve(fqdn)
    return targets
