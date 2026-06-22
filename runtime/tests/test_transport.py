from locdist.transport import (
    get_transport,
)


def main():

    transport1 = get_transport()

    transport2 = get_transport()

    assert transport1 is transport2

    print(
        "✓ Transport singleton OK"
    )


if __name__ == "__main__":
    main()