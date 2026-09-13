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
        'kind',
        'status',
        'error',
        'user_id',
    ];

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
