"""Offline training pipeline for the local retrieval model.

Core modules are stdlib-only (dataset, metrics, gate, score_params,
registry); the heavyweight training/serving dependencies live behind the
`train`/`serve` extras and are imported only by the entry scripts.
"""
