<div>
    <div class="bg-paper border-b border-stone-200">
        <div class="max-w-[1600px] mx-auto py-6 px-4 sm:px-6 lg:px-8 flex items-center justify-between flex-wrap gap-3">
            <div>
                <h2 class="font-semibold text-xl text-stone-800">Activity</h2>
                <p class="text-sm text-stone-500 mt-1">What's happened across every machine you can access, most recent first.</p>
            </div>
            <select wire:model.live="machineId" class="border-stone-300 rounded-md shadow-sm text-sm focus:border-tan-400 focus:ring-tan-400">
                <option value="">All machines</option>
                @foreach ($machines as $machine)
                    <option value="{{ $machine->id }}">{{ $machine->name }}</option>
                @endforeach
            </select>
        </div>
    </div>

    <div class="max-w-[1600px] mx-auto py-8 px-4 sm:px-6 lg:px-8">
        @if ($entries->isEmpty())
            <div class="rounded-xl border border-dashed border-stone-300 bg-paper p-12 text-center">
                <p class="text-stone-500">Nothing to show yet — this fills in as machines are browsed.</p>
            </div>
        @else
            <div class="rounded-xl bg-paper shadow-soft border border-stone-200 overflow-hidden">
                <table class="w-full text-sm">
                    <thead class="bg-stone-50 text-stone-400 text-xs uppercase tracking-wide">
                        <tr>
                            <th class="text-left font-medium px-4 py-2 w-40">When</th>
                            <th class="text-left font-medium px-4 py-2 w-36">Machine</th>
                            <th class="text-left font-medium px-4 py-2 w-20">Method</th>
                            <th class="text-left font-medium px-4 py-2">Path</th>
                            <th class="text-right font-medium px-4 py-2 w-20">Status</th>
                        </tr>
                    </thead>
                    <tbody>
                        @foreach ($entries as $entry)
                            <tr wire:key="activity-{{ $entry['machine']->id }}-{{ $loop->index }}" class="border-b border-stone-50">
                                <td class="px-4 py-2 text-stone-500 text-xs whitespace-nowrap">{{ \Illuminate\Support\Carbon::parse($entry['time'])->diffForHumans() }}</td>
                                <td class="px-4 py-2">
                                    <span class="inline-flex items-center gap-1.5">
                                        <span class="w-2 h-2 rounded-full shrink-0" style="background-color: {{ $entry['machine']->color }}"></span>
                                        {{ $entry['machine']->name }}
                                    </span>
                                </td>
                                <td class="px-4 py-2 font-mono text-xs text-stone-500">{{ $entry['method'] }}</td>
                                <td class="px-4 py-2 font-mono text-xs text-stone-700 truncate max-w-0">{{ $entry['path'] }}</td>
                                <td @class([
                                    'px-4 py-2 text-right text-xs font-medium',
                                    'text-green-600' => $entry['status'] < 300,
                                    'text-amber-600' => $entry['status'] >= 300 && $entry['status'] < 500,
                                    'text-red-600' => $entry['status'] >= 500,
                                ])>{{ $entry['status'] }}</td>
                            </tr>
                        @endforeach
                    </tbody>
                </table>
            </div>
        @endif
    </div>
</div>
