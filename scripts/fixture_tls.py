# SPDX-License-Identifier: GPL-2.0-only
"""Verified, explicitly imported client identity for synthetic box listeners."""
import ssl


def client_context(root, box='001122334455'):
    context = ssl.create_default_context(cafile=str(root / 'certs/server/teddy-cert.pem'))
    context.load_cert_chain(root / f'certs/client/{box}/teddy-cert.pem',
                            root / f'certs/client/{box}/teddy-key.pem')
    return context
