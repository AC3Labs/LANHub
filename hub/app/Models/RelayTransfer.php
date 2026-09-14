<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\BelongsTo;

class RelayTransfer extends Model
{
    protected $fillable = [
        'source_machine_id',
        'destination_machine_id',
        'source_path',
        'destination_path',
        'bytes_transferred',
        'kind',
        'status',
        'started_at',
        'completed_at',
        'error',
        'user_id',
    ];

    protected function casts(): array
    {
        return [
            'started_at' => 'datetime',
            'completed_at' => 'datetime',
        ];
    }

    public function sourceMachine(): BelongsTo
    {
        return $this->belongsTo(Machine::class, 'source_machine_id');
    }

    public function destinationMachine(): BelongsTo
    {
        return $this->belongsTo(Machine::class, 'destination_machine_id');
    }

    public function user(): BelongsTo
    {
        return $this->belongsTo(User::class);
    }
}
