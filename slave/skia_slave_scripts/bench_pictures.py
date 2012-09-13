#!/usr/bin/env python
# Copyright (c) 2012 The Chromium Authors. All rights reserved.
# Use of this source code is governed by a BSD-style license that can be
# found in the LICENSE file.

""" Run the Skia bench_pictures executable. """

from utils import misc
from build_step import BuildStep
from run_bench import BuildArgs
from run_bench import RunBench
from run_bench import PreBench
import os
import sys

class BenchPictures(RunBench):
  def _BuildDataFile(self, perf_dir, config):
    return '%s_skp_%s' % (super(BenchPictures, self)._BuildDataFile(perf_dir),
                          config)

  def _DoBenchPictures(self, config):
    cmd = [self._PathToBinary('bench_pictures'), self._skp_dir,
           '--device', config]
    if self._perf_data_dir:
      PreBench(self._perf_data_dir)
      cmd += BuildArgs(self.BENCH_REPEAT_COUNT,
                       self._BuildDataFile(self._perf_data_dir, config))
    misc.Bash(cmd)

  def _Run(self, args):
    self._DoBenchPictures('bitmap')
    gyp_defines = os.environ.get('GYP_DEFINES', '')
    if 'skia_gpu=0' not in gyp_defines:
      self._DoBenchPictures('gpu')


if '__main__' == __name__:
  sys.exit(BuildStep.Run(BenchPictures))

