import torch


def tensor_to_bytes(tensor: torch.Tensor) -> bytes:
    cpu_tensor = tensor.detach().cpu().contiguous()
    try:
        return cpu_tensor.numpy().tobytes()
    except RuntimeError as error:
        if "Numpy is not available" not in str(error):
            raise
    return bytes(cpu_tensor.view(torch.uint8).tolist())
