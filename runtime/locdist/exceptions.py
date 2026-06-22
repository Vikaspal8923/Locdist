# runtime/locdist/exceptions.py


class LocDistError(Exception):
    """
    Base exception for all LDGCC Runtime errors.
    """


# =====================================================
# Configuration
# =====================================================

class ConfigError(LocDistError):
    """
    Runtime configuration is invalid.
    """


# =====================================================
# Gradient Processing
# =====================================================

class GradientError(LocDistError):
    """
    Gradient extraction or reconstruction failed.
    """


# =====================================================
# Serialization
# =====================================================

class SerializationError(LocDistError):
    """
    Proto serialization/deserialization failed.
    """


# =====================================================
# Transport
# =====================================================

class TransportError(LocDistError):
    """
    Runtime ↔ Worker Service communication failed.
    """


class ConnectionError(TransportError):
    """
    Cannot connect to Worker Service.
    """


class SynchronizationError(TransportError):
    """
    Gradient synchronization failed.
    """