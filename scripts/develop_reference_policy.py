# SPDX-License-Identifier: GPL-2.0-only
"""Bound the pinned C oracle's unlocked box-list reload window, never Go reads."""
import functools
import time


def wait_for_overlay(lookup, box):
    deadline = time.monotonic() + 2
    while True:
        try:
            return lookup(box)
        except StopIteration:
            if time.monotonic() >= deadline:
                raise
            time.sleep(0.02)


def pytest_collection_modifyitems(items):
    # C handleApiGetBoxes reads overlay fields without MUTEX_SETTINGS, while
    # settings_load resets them. The simulator may briefly miss its own box
    # between two settings reads. Keep all value assertions and persistent
    # missing-box failures intact; do not patch the pinned upstream checkout.
    modules = {item.module for item in items if item.module.__name__.rsplit('.', 1)[-1] == 'test_box_profiles'}
    for module in modules:
        lookup = module.overlay_id

        @functools.wraps(lookup)
        def overlay_id(box, _lookup=lookup):
            return wait_for_overlay(_lookup, box)

        module.overlay_id = overlay_id
