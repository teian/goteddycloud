# SPDX-License-Identifier: GPL-2.0-only
"""Keep the standard-library cipher gap visible as a strict expected failure."""
import pytest

def pytest_collection_modifyitems(items):
    for item in items:
        if item.name == 'test_legacy_cipher_suites_are_accepted[AES256-SHA256]':
            item.add_marker(pytest.mark.xfail(strict=True, reason='Go crypto/tls does not implement TLS_RSA_WITH_AES_256_CBC_SHA256 (0x003d); recorded compatibility deviation'))
