'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';

export function Nav() {
  const pathname = usePathname();

  return (
    <nav className="bg-slate-900 text-white shadow-lg">
      <div className="max-w-7xl mx-auto px-4">
        <div className="flex items-center justify-between h-14">
          <div className="flex items-center gap-8">
            <Link href="/" className="text-lg font-bold tracking-tight">
              <span className="text-indigo-400">q</span>factory
            </Link>
            <div className="flex items-center gap-1">
              <Link
                href="/runs"
                className={`px-3 py-2 rounded-md text-sm font-medium transition-colors ${
                  pathname?.startsWith('/runs')
                    ? 'bg-slate-800 text-white'
                    : 'text-slate-300 hover:bg-slate-800 hover:text-white'
                }`}
              >
                Runs
              </Link>
            </div>
          </div>
          <div className="text-xs text-slate-400">Control Plane</div>
        </div>
      </div>
    </nav>
  );
}
