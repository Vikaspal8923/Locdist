import os

from locdist.transport import (
    TransportClient,
    get_transport,
)


def main():
    os.environ["LDGCC_JOB_ID"] = "job-test"
    os.environ["LDGCC_WORKER_ID"] = "worker-test"
    os.environ["LDGCC_WORKER_HOST"] = "127.0.0.1"
    os.environ["LDGCC_WORKER_PORT"] = "50051"

    transport1 = get_transport()

    transport2 = get_transport()

    assert transport1 is transport2
    assert transport1.transport_peer == "worker"

    os.environ["LDGCC_MASTER_HOST"] = "10.0.0.7"
    os.environ["LDGCC_MASTER_PORT"] = "60051"
    os.environ["LDGCC_SYNC_TARGET"] = "master"

    direct_master = TransportClient()
    assert direct_master.transport_peer == "master"
    assert direct_master.address == "10.0.0.7:60051"
    assert direct_master._rpc_timeout_for_round(1) is None
    assert direct_master._rpc_timeout_for_round(2) == direct_master.config.rpc_timeout_seconds

    print(
        "✓ Transport singleton OK"
    )
    print(
        "✓ Direct-to-master transport target OK"
    )
    print(
        "✓ First-round startup grace timeout behavior OK"
    )


if __name__ == "__main__":
    main()
