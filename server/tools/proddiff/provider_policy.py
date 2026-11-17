"""Comparison-only response policy; rendered content and headers stay untouched."""


def wait_for_fresh_provider():
    """Wait for rendering while retaining normal publication/last-good writes."""
    from backend.utils import provider_response
    provider_response.RESPONSE_SECONDS = None
