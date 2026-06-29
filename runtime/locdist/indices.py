import struct


UINT32_MAX = (1 << 32) - 1


def pack_u32_indices(indices: list[int]) -> bytes:
    if not indices:
        return b""
    for index in indices:
        if index < 0 or index > UINT32_MAX:
            raise ValueError(
                "top-k uint32 indices support values from 0 to 4,294,967,295"
            )
    return struct.pack("<" + "I" * len(indices), *indices)


def unpack_u32_indices(data: bytes) -> list[int]:
    if not data:
        return []
    if len(data) % 4 != 0:
        raise ValueError("packed uint32 indices byte length must be divisible by 4")
    count = len(data) // 4
    return list(struct.unpack("<" + "I" * count, data))
