"""Close real download sockets after headers and one byte of a large response."""
import base64
import http.client
import random
import socket
import time
from urllib.parse import urlsplit

from compare import Policy, differences, database_diffs, file_diffs, response_diffs
from downloads import capture_headers
from harness import Pair, Reply, json_body
from sources import Source
from cases.generate import FILENAMES
from cases.generator_edges import generators
from cases.profile_concurrency import record
from cases.profile_generators import GeneratedOnly, generated_errors
from cases.subscriptions import require


def transfer_errors(value):
    expected = {'status': 200, 'content_length_is_large': True, 'one_byte_read': True,
                'unread_response_bytes': True, 'receive_buffer_is_small': True, 'socket_closed': True,
                'length_matches_own_completed_materialization': True}
    return differences(expected, value, 'download.disconnected.transfer')


def profile(service, payload):
    if service == 'mihomo':
        return {'mihomo': {'custom_config': 'future_download_payload: ' + payload + '\n'}}
    if service in ('surge', 'loon'):
        comments = '\n'.join('# ' + payload[n:n+96] for n in range(0, len(payload), 96))
        return {service: {'custom_config': '[General]\n' + comments + '\n'}}
    if service == 'mosdns':
        return {'mosdns': {'custom_config': 'future_download_payload: ' + payload + '\n'}}
    return {'oxidns': {'additional_files': [{'path': 'large-download.txt', 'content': payload, 'encoding': 'text'}]}}


def cancelled(binary, recorder):
    payload = base64.b64encode(random.Random(20261117).randbytes(3 * 1024 * 1024)).decode()
    for rid, route, path, service, preview in generators():
        if preview:
            continue
        with Source(b'large downloads use only synthetic local text') as source, Pair(binary, recorder,
                options={'profile': profile(service, payload)}) as pair:
            complete = pair.request(rid + '-S01', route, path, body=json_body({'base_url': 'http://panel.example'}),
                expect=200, writes=True, filename=FILENAMES[service], capture=capture_headers,
                variant='large-disconnect-materialization-precondition')
            require(len(complete[0].body) > 1024 * 1024, 'real full reference download exceeds the receive window by over one MiB')
            registered = pair.route_available(route)
            available = registered and not pair.unimplemented_mutations
            outputs, violations = [[], []], []
            for index in range(2 if available else 1):
                saved = {'mihomo': 'config.yaml', 'surge': 'config.conf', 'loon': 'loon.lcf'}.get(service, '')
                observer = GeneratedOnly(pair, source, 'default', saved)
                before = observer.snapshot(index)
                target = urlsplit(pair.bases[index])
                conn = http.client.HTTPConnection(target.hostname, target.port, timeout=30)
                conn.connect()
                conn.sock.setsockopt(socket.SOL_SOCKET, socket.SO_RCVBUF, 512)
                small = conn.sock.getsockopt(socket.SOL_SOCKET, socket.SO_RCVBUF) <= 16 * 1024
                body = json_body({'base_url': 'http://panel.example'})
                conn.request('POST', path, body=body.encode(), headers={'Content-Type': 'application/json',
                             'Authorization': 'Bearer ' + pair.tokens[index], 'Connection': 'close'})
                response = conn.getresponse()
                headers = {k.lower(): v for k, v in response.getheaders()}
                length = int(headers.get('content-length', '0'))
                prefix = response.read(1)
                # Response file holds another socket reference: explicitly shut
                # down the real transport before closing both owning objects.
                transport = response.fp.raw._sock
                transport.shutdown(socket.SHUT_RDWR)
                response.close()
                conn.close()
                observed = {'status': response.status, 'content_length_is_large': length > 1024 * 1024,
                    'one_byte_read': len(prefix) == 1, 'unread_response_bytes': length > len(prefix),
                    'receive_buffer_is_small': small, 'socket_closed': transport.fileno() == -1,
                    'length_matches_own_completed_materialization': length == len(complete[index].body)}
                own = transfer_errors(observed)
                own += generated_errors(before, observer.snapshot(index), 'default', saved, complete[index].body)
                violations.append(own)
                outputs[index] = [Reply(response.status, headers, b'')]
                if index == 0:
                    require(not own, 'actual one-byte cancellation retains complete generated file and all other SQL/files: ' + repr(own[:5]))
            pair.policy.end = time.time()
            diffs = []
            if available:
                diffs += violations[1]
                header_policy = Policy(start=pair.policy.start, end=pair.policy.end)
                # Full lengths were validated independently against each
                # materialization above. Bind only real header validators;
                # do not invent a downloaded body for this cancelled request.
                header_replies = [Reply(batch[0].status, {k: v for k, v in batch[0].headers.items()
                                  if k != 'content-length'}, b'') for batch in outputs]
                diffs += capture_headers(header_replies, header_policy)
                diffs += response_diffs(*header_replies, header_policy, method='HEAD')
                diffs += database_diffs(*pair.dirs, pair.policy) + file_diffs(*pair.dirs, pair.policy)
            record(pair, rid + '-B04', route, 'real-large-download-socket-shutdown-after-one-byte', available, registered, diffs, outputs)
            pair.recorder.results[-1].update(response_body_intentionally_unread=True,
                python_bytes_read=1, python_complete_body_bytes=len(complete[0].body))


GROUPS = {'generated-download-real-client-disconnect': cancelled}
