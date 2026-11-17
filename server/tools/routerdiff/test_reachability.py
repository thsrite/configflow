"""Source reachability cannot silently become executed HTTP coverage."""
import tempfile
from pathlib import Path
import unittest

import clients
import fixed_sources
import workflows
import cases
import upstream
from audits import declared as variant_audits, evaluate
from inventory import load
from reachability import declared
from run import report


class ReachabilityTests(unittest.TestCase):
    def controls(self):
        actual = {(world, case.ident): case for world, _, groups in fixed_sources.router_worlds()
                  for group in groups for case in group()}
        for world, group in (('discovery', clients.discovery), ('discovery-failures', clients.discovery_failures),
                             ('guards', cases.profile_selection_failures), ('runtime-reads', workflows.runtime_reads),
                             ('upstream-linked', upstream.linked)):
            actual.update({(world, case.ident): case for case in group()})
        return actual

    def observations(self):
        controls = {(world, case): status for proof in declared().values() for world, case, status in proof.controls}
        # Deliberate oracle-only observations: no H execution tags, no claim
        # that this self-test is a reference product run.
        return [{'world': world, 'case': case, 'entries': [], 'python': {'status': status},
                 'status': 'go-unimplemented'} for (world, case), status in controls.items()]

    def test_all_proofs_need_actual_named_controls_but_never_tag_an_unexecuted_branch(self):
        actual = self.controls()
        proofs = declared()
        self.assertEqual(set(proofs), {'H0034', 'H0041', 'H0061', 'R001-F08', 'R001-F09'})
        self.assertFalse(proofs.keys() & variant_audits().keys())
        self.assertTrue(proofs.keys() <= load()[1].keys())
        for ident, proof in proofs.items():
            self.assertTrue(proof.rationale)
            self.assertTrue(proof.sources)
            self.assertTrue(proof.controls)
            for world, name, status in proof.controls:
                case = actual[world, name]
                self.assertEqual(case.expected, status)
                self.assertNotIn(ident, case.entries)
            frames = [{'world': world, 'case': name, 'entries': [], 'python': {'status': status}}
                      for world, name, status in proof.controls]
            verdict = evaluate(frames)[ident]
            self.assertEqual(verdict['audit'], proof.disposition)
            self.assertFalse(verdict['execution_claim'])
            pending = 'source-proof-controls-pending' if proof.disposition == 'source-unreachable' else 'review-proof-controls-pending'
            self.assertEqual(evaluate(frames[:-1])[ident]['audit'], pending)
            frames[-1]['python']['status'] = 500
            self.assertEqual(evaluate(frames)[ident]['audit'], pending)

    def test_report_counts_source_proofs_separately_from_tested_variants(self):
        with tempfile.TemporaryDirectory(prefix='routerdiff-source-proof-') as directory:
            result = report(Path(directory) / 'report.json', self.observations(), set(), announce=False)
        self.assertEqual(result['audited_entries'], 0)
        self.assertEqual(result['source_unreachable_entries'], 3)
        self.assertEqual(result['review_superseded_entries'], 2)
        self.assertEqual(result['remaining_audits'], 2296)
        self.assertEqual(result['referenced_entries'], 0)
        self.assertEqual(result['implemented_routes'], 0)
        self.assertFalse(result['complete'])
        self.assertNotIn('compared', result['recipe_counts'])


if __name__ == '__main__':
    unittest.main()
