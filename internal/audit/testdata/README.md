# Test fixtures

witness/ holds the checkpoint witness's 54 vectors, copied verbatim from
Judgment-Pack/judgment-pack-gateway corpus/witness/ at commit
c916ee933dff0b72ebf7741c1ce8022487bfb807 (gateway ADR-0013, PR 1 of 6). Each is
a reading of the gateway's SPEC.md §8.6: the trail being verified, the witness
keys supplied, statements files, a head file when there is one, and the answer
expected. They are the gateway's frozen data, not this runtime's: a vector that
fails here is a question for the gateway's specification before it is one for
this reader, and a change to one is made there first and copied again.

witness.lock.json records that source and, for every file, its byte count and
SHA-256. TestTheWitnessVectorsAreTheGatewaysCopy holds the directory to it, so
a file added, removed or edited here without the lock fails, and the lock names
the commit a new copy must come from.

The statements are signed under the gateway corpus's published test seed, which
signs nothing real; only public keys are carried in the vectors.
