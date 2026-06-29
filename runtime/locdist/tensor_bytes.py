import torch


def tensor_to_bytes(tensor: torch.Tensor) -> bytes:
    cpu_tensor = tensor.detach().cpu().contiguous()
    return bytes(cpu_tensor.view(torch.uint8).tolist())
