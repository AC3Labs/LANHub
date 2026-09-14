<div>
    <div class="bg-paper border-b border-stone-200">
        <div class="max-w-[1600px] mx-auto py-6 px-4 sm:px-6 lg:px-8">
            <a href="{{ route('agents') }}" wire:navigate class="text-xs text-stone-400 hover:text-stone-600">&larr; Agents</a>
            <div class="flex items-center gap-3 mt-1">
                <span class="w-2.5 h-2.5 rounded-full shrink-0" style="background-color: {{ $machine->color }}"></span>
                <h2 class="font-semibold text-xl text-stone-800">{{ $machine->name }} — Transfer Log</h2>
            </div>
            <p class="text-sm text-stone-500 mt-1">Every cross-machine transfer sent from or received by this machine, most recent first.</p>
        </div>
    </div>

    <div class="max-w-[1600px] mx-auto py-8 px-4 sm:px-6 lg:px-8">
        @if ($transfers->isEmpty())
            <div class="rounded-xl border border-dashed border-stone-300 bg-paper p-12 text-center">
                <p class="text-stone-500">No transfers involving this machine yet.</p>
            </div>
        @else
            <div class="rounded-xl bg-paper shadow-soft border border-stone-200 overflow-hidden">
                <table class="w-full text-sm">
                    <thead class="bg-stone-50 text-stone-400 text-xs uppercase tracking-wide">
                        <tr>
                            <th class="text-left font-medium px-4 py-2">File</th>
                            <th class="text-left font-medium px-4 py-2 w-24">Direction</th>
                            <th class="text-left font-medium px-4 py-2 w-40">Machine</th>
                            <th class="text-left font-medium px-4 py-2 w-20">Size</th>
                            <th class="text-left font-medium px-4 py-2 w-40">When</th>
                            <th class="text-right font-medium px-4 py-2 w-24">Status</th>
                        </tr>
                    </thead>
                    <tbody>
                        @foreach ($transfers as $transfer)
                            @php
                                $sent = $transfer->source_machine_id === $machine->id;
                                $file = basename(str_replace('\\', '/', $sent ? $transfer->destination_path : $transfer->source_path));
                                $other = $sent ? $transfer->destinationMachine : $transfer->sourceMachine;
                            @endphp
                            <tr wire:key="transfer-{{ $transfer->id }}" class="border-b border-stone-50">
                                <td class="px-4 py-2 font-mono text-xs text-stone-700 truncate max-w-0">{{ $file }}</td>
                                <td class="px-4 py-2">
                                    <span @class([
                                        'inline-flex items-center gap-1 text-xs font-medium',
                                        'text-blue-600' => $sent,
                                        'text-emerald-600' => ! $sent,
                                    ])>
                                        <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                            @if ($sent)
                                                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 19V5m0 0l-6 6m6-6l6 6"/>
                                            @else
                                                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 5v14m0 0l-6-6m6 6l6-6"/>
                                            @endif
                                        </svg>
                                        {{ $sent ? 'Sent' : 'Received' }}
                                    </span>
                                </td>
                                <td class="px-4 py-2">
                                    <span class="inline-flex items-center gap-1.5 text-xs text-stone-600">
                                        <span class="w-2 h-2 rounded-full shrink-0" style="background-color: {{ $other?->color }}"></span>
                                        {{ $sent ? 'to' : 'from' }} {{ $other?->name ?? 'Unknown' }}
                                    </span>
                                </td>
                                <td class="px-4 py-2 text-xs text-stone-500">
                                    {{ $transfer->bytes_transferred ? human_filesize($transfer->bytes_transferred) : '—' }}
                                </td>
                                <td class="px-4 py-2 text-stone-500 text-xs whitespace-nowrap">{{ $transfer->created_at->diffForHumans() }}</td>
                                <td @class([
                                    'px-4 py-2 text-right text-xs font-medium capitalize',
                                    'text-green-600' => $transfer->status === 'done',
                                    'text-amber-600' => in_array($transfer->status, ['queued', 'downloading', 'uploading']),
                                    'text-red-600' => $transfer->status === 'error',
                                ])>{{ $transfer->status }}</td>
                            </tr>
                        @endforeach
                    </tbody>
                </table>
            </div>

            <div class="mt-4">
                {{ $transfers->links() }}
            </div>
        @endif
    </div>
</div>
